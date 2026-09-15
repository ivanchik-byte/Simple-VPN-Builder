package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/payment"
	"github.com/skip2/go-qrcode"
)

type BotEngine struct {
	bot              *tgbotapi.BotAPI
	cpClient         *client.CPClient
	paymentMgr       *payment.Manager
	lang             i18n.Language
	userStates       map[int64]string // chat_id -> state (e.g. awaiting_promo, awaiting_email_otp)
	userPendingEmail map[int64]string // chat_id -> pending email for OTP verification
}

func NewBotEngine(bot *tgbotapi.BotAPI, cpClient *client.CPClient, paymentMgr *payment.Manager) *BotEngine {
	return &BotEngine{
		bot:              bot,
		cpClient:         cpClient,
		paymentMgr:       paymentMgr,
		lang:             i18n.EN,
		userStates:       make(map[int64]string),
		userPendingEmail: make(map[int64]string),
	}
}

func (e *BotEngine) RegisterBotCommands() {
	if e.bot == nil {
		return
	}
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Главное меню / Main Menu"},
		{Command: "status", Description: "Моя подписка и трафик / Subscription status"},
		{Command: "plans", Description: "Тарифы и оплата / Buy subscription"},
		{Command: "trial", Description: "Бесплатный тест / Free trial"},
		{Command: "connect", Description: "Ключи и подписка / Get VPN config & QR"},
		{Command: "devices", Description: "Приложения и настройка / Setup guides"},
		{Command: "servers", Description: "Список серверов / Server locations"},
		{Command: "referral", Description: "Реферальная программа / Invite friends"},
		{Command: "promo", Description: "Ввести промокод / Redeem promo code"},
		{Command: "email", Description: "Привязать почту / Link recovery email"},
		{Command: "restore", Description: "Восстановить доступ / Restore account by email"},
		{Command: "reset", Description: "Сбросить ключи / Rotate keys"},
		{Command: "support", Description: "Поддержка / Support"},
		{Command: "help", Description: "Справка и команды / Help & FAQ"},
	}
	_, _ = e.bot.Request(tgbotapi.NewSetMyCommands(commands...))
}

func (e *BotEngine) Start(ctx context.Context) error {
	e.RegisterBotCommands()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := e.bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			e.handleUpdate(ctx, update)
		}
	}
}

func (e *BotEngine) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.PreCheckoutQuery != nil {
		e.handlePreCheckoutQuery(update.PreCheckoutQuery)
		return
	}

	if update.Message != nil && update.Message.SuccessfulPayment != nil {
		e.handleSuccessfulPayment(ctx, update.Message)
		return
	}

	if update.CallbackQuery != nil {
		e.handleCallbackQuery(ctx, update.CallbackQuery)
		return
	}

	if update.Message != nil {
		e.handleMessage(ctx, update.Message)
	}
}

func (e *BotEngine) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	t := i18n.GetBundle(e.lang)
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	// Ban guard: if user is banned by admin, block actions
	if userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID); err == nil && userRes != nil {
		if userRes.User.IsBanned.Valid && userRes.User.IsBanned.Bool {
			reason := "Suspended by admin"
			if userRes.User.BanReason.Valid && userRes.User.BanReason.String != "" {
				reason = userRes.User.BanReason.String
			}
			e.sendMessage(chatID, fmt.Sprintf(t.BannedMessage, reason), nil)
			return
		}
	}

	// Check if awaiting user state
	if state, ok := e.userStates[chatID]; ok {
		switch state {
		case "awaiting_promo":
			delete(e.userStates, chatID)
			e.processPromoCode(ctx, chatID, text)
			return
		case "awaiting_email":
			delete(e.userStates, chatID)
			e.processLinkEmail(ctx, chatID, text)
			return
		case "awaiting_email_otp":
			e.processVerifyEmailOTP(ctx, chatID, text, msg.From)
			return
		case "awaiting_restore":
			delete(e.userStates, chatID)
			e.processRestoreAccount(ctx, chatID, msg.From, text)
			return
		case "awaiting_restore_otp":
			e.processVerifyRestoreOTP(ctx, chatID, text, msg.From)
			return
		}
	}

	if strings.HasPrefix(text, "/start") {
		refCode := ""
		parts := strings.Split(text, " ")
		if len(parts) > 1 {
			refCode = parts[1]
		}
		e.handleStart(ctx, msg, refCode)
		return
	}

	if strings.HasPrefix(text, "/email") {
		parts := strings.Fields(text)
		if len(parts) > 1 {
			e.processLinkEmail(ctx, chatID, parts[1])
			return
		}
		e.userStates[chatID] = "awaiting_email"
		e.sendMessage(chatID, t.EmailPrompt, nil)
		return
	}

	if strings.HasPrefix(text, "/restore") {
		parts := strings.Fields(text)
		if len(parts) > 1 {
			e.processRestoreAccount(ctx, chatID, msg.From, parts[1])
			return
		}
		e.userStates[chatID] = "awaiting_restore"
		e.sendMessage(chatID, t.RestorePrompt, nil)
		return
	}

	switch text {
	case "/status", t.BtnStatus:
		e.handleStatus(ctx, chatID)
	case "/buy", "/plans", "/tariffs", t.BtnRenew, t.BtnPlans, "Тарифы", "Plans":
		e.handleBuy(ctx, chatID)
	case "/trial", "/test", t.BtnGetTrial, "Попробовать бесплатно", "Get Free Trial":
		e.handleClaimTrial(ctx, chatID, msg.From.UserName, "")
	case "/reset", t.BtnResetKeys, "Сбросить ключи", "Reset Keys", "Перевыпустить ключи / Сбросить подключение", "Rotate Keys / Reset Connection":
		e.handleResetPrompt(ctx, chatID)
	case "/devices", "/apps", "/connect", "/config", "/sub", t.BtnDeviceWizard, "Подключение по устройствам", "Device Setup Wizard":
		e.handleDeviceWizard(ctx, chatID)
	case "/servers", "/nodes", t.BtnNodes, "Список серверов", "Server List":
		e.handleServerList(ctx, chatID)
	case "/promo", t.BtnEnterPromo:
		e.userStates[chatID] = "awaiting_promo"
		e.sendMessage(chatID, t.PromoPrompt, nil)
	case t.BtnLinkEmail, "Привязать Email", "Link Recovery Email":
		e.userStates[chatID] = "awaiting_email"
		e.sendMessage(chatID, t.EmailPrompt, nil)
	case t.BtnRestore, "Восстановить по Email", "Restore Account":
		e.userStates[chatID] = "awaiting_restore"
		e.sendMessage(chatID, t.RestorePrompt, nil)
	case "/ref", "/referral", "/invite", t.BtnReferral, "Реферальная программа", "Referral Program":
		e.handleReferral(ctx, chatID)
	case "/support", "/admin", t.BtnSupport, "Поддержка", "Support":
		e.handleSupport(ctx, chatID)
	case "/help", "/commands", t.BtnHelp, "Инструкция", "Setup Guide", "Справка", "Help":
		e.handleHelp(ctx, chatID)
	default:
		e.handleStart(ctx, msg, "")
	}
}

func (e *BotEngine) handleStart(ctx context.Context, msg *tgbotapi.Message, refCode string) {
	t := i18n.GetBundle(e.lang)
	chatID := msg.Chat.ID

	// Upsert lead asynchronously to record CRM lead profile on /start
	go func() {
		fromUser := msg.From
		username := ""
		firstName := ""
		lastName := ""
		langCode := "en"
		if fromUser != nil {
			username = fromUser.UserName
			firstName = fromUser.FirstName
			lastName = fromUser.LastName
			if fromUser.LanguageCode != "" {
				langCode = fromUser.LanguageCode
			}
		}
		_ = e.cpClient.UpsertLead(context.Background(), client.TelegramLeadParams{
			TelegramID:       chatID,
			TelegramUsername: username,
			FirstName:        firstName,
			LastName:         lastName,
			LanguageCode:     langCode,
			ReferrerCode:     refCode,
		})
	}()

	// Query Control Plane for user
	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil {
		e.sendMessage(chatID, "Error communicating with control plane. Please try again later.", nil)
		return
	}

	customReplies, _ := e.cpClient.GetBotReplies(ctx)

	if userRes != nil {
		if userRes.User.IsBanned.Valid && userRes.User.IsBanned.Bool {
			reason := "Banned by admin"
			if userRes.User.BanReason.Valid && userRes.User.BanReason.String != "" {
				reason = userRes.User.BanReason.String
			}
			e.sendMessage(chatID, fmt.Sprintf(t.BannedMessage, reason), nil)
			return
		}

		referralEnabled := true
		if customReplies != nil && customReplies["referral_enabled"] == "false" {
			referralEnabled = false
		}

		var lastRow []tgbotapi.InlineKeyboardButton
		lastRow = append(lastRow, tgbotapi.NewInlineKeyboardButtonData(t.BtnEnterPromo, "action:promo"))
		if referralEnabled {
			lastRow = append(lastRow, tgbotapi.NewInlineKeyboardButtonData("🎁 "+t.BtnReferral, "action:referral"))
		}

		rows := [][]tgbotapi.InlineKeyboardButton{
			{
				tgbotapi.NewInlineKeyboardButtonData("⚡ "+t.BtnConnectOneClick, "action:devices"),
				tgbotapi.NewInlineKeyboardButtonData("💎 "+t.BtnRenew, "action:buy"),
			},
			{
				tgbotapi.NewInlineKeyboardButtonData("📊 "+t.BtnStatus, "action:status"),
				tgbotapi.NewInlineKeyboardButtonData("🔄 "+t.BtnResetKeys, "action:reset"),
			},
			{
				tgbotapi.NewInlineKeyboardButtonData("🌐 "+t.BtnNodes, "action:nodes"),
				tgbotapi.NewInlineKeyboardButtonData("📖 "+t.BtnHelp, "action:help"),
			},
			lastRow,
		}

		if !userRes.User.Email.Valid || userRes.User.Email.String == "" || strings.HasSuffix(userRes.User.Email.String, "@t.me") {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("✉️ "+t.BtnLinkEmail, "action:email"),
			})
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

		welcomeTpl := t.WelcomeActiveUser
		if val, ok := customReplies["welcome_active_user"]; ok && val != "" {
			welcomeTpl = val
		}

		expiresStr := "Unlimited"
		if userRes.User.ExpiresAt.Valid && !userRes.User.ExpiresAt.Time.IsZero() {
			expiresStr = userRes.User.ExpiresAt.Time.Format("2006-01-02 15:04")
		}

		baseURL := e.getPublicBaseURL()
		fullSubURL := userRes.SubscriptionURL
		if strings.HasPrefix(fullSubURL, "/") {
			fullSubURL = baseURL + fullSubURL
		}

		text := welcomeTpl
		if strings.Contains(welcomeTpl, "%s") {
			text = fmt.Sprintf(welcomeTpl,
				i18n.FormatBytes(userRes.User.TrafficUsed.Int64),
				i18n.FormatBytes(userRes.User.TrafficLimit.Int64),
				expiresStr,
				fullSubURL,
			)
		}
		e.sendStartMessage(ctx, chatID, text, &keyboard, customReplies)
		return
	}

	welcomeNew := t.WelcomeNewUser
	if refCode != "" && customReplies != nil {
		if val, ok := customReplies["welcome_referral"]; ok && val != "" {
			welcomeNew = val
		}
	}
	if welcomeNew == t.WelcomeNewUser && customReplies != nil {
		if val, ok := customReplies["welcome_new_user"]; ok && val != "" {
			welcomeNew = val
		}
	}

	// New user: check if free trial is available
	trialPlan, _ := e.cpClient.GetTrialPlan(ctx)
	if trialPlan != nil && trialPlan.IsActive.Bool {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🎁 "+t.BtnGetTrial, fmt.Sprintf("action:claim_trial:%s", refCode)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("💎 "+t.BtnPlans, "action:buy"),
				tgbotapi.NewInlineKeyboardButtonData("📖 "+t.BtnHelp, "action:help"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🔑 "+t.BtnRestore, "action:restore"),
			),
		)
		e.sendStartMessage(ctx, chatID, welcomeNew, &keyboard, customReplies)
		return
	}

	// No trial: direct to purchase
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💎 "+t.BtnPlans, "action:buy"),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📖 "+t.BtnHelp, "action:help"),
				tgbotapi.NewInlineKeyboardButtonData("🔑 "+t.BtnRestore, "action:restore"),
			)[0],
		),
	)
	e.sendStartMessage(ctx, chatID, welcomeNew+"\n\n"+t.NoTrialAvailable, &keyboard, customReplies)
}

func (e *BotEngine) sendTemplatedMessage(ctx context.Context, chatID int64, replyKey string, text string, keyboard *tgbotapi.InlineKeyboardMarkup, customReplies map[string]string) {
	if customReplies == nil {
		customReplies, _ = e.cpClient.GetBotReplies(ctx)
	}

	var mediaURL string
	if customReplies != nil {
		mediaURL = strings.TrimSpace(customReplies[replyKey+"_media"])
		if mediaURL == "" && (replyKey == "welcome_new_user" || replyKey == "welcome_referral" || replyKey == "welcome_active_user") {
			mediaURL = strings.TrimSpace(customReplies["welcome_banner_url"])
			if mediaURL == "" {
				mediaURL = strings.TrimSpace(customReplies["bot_welcome_banner"])
			}
		}
	}

	if mediaURL != "" {
		caption := e.applyTemplateTags(ctx, chatID, text)
		photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(mediaURL))
		photo.Caption = caption
		if strings.Contains(caption, "<") && strings.Contains(caption, ">") {
			photo.ParseMode = tgbotapi.ModeHTML
		}
		if keyboard != nil {
			photo.ReplyMarkup = keyboard
		}
		if _, sendErr := e.bot.Send(photo); sendErr == nil {
			return
		} else {
			slog.Warn("Failed to send templated photo message, falling back to text", "key", replyKey, "error", sendErr)
		}
	}

	e.sendMessage(chatID, text, keyboard)
}

func (e *BotEngine) sendStartMessage(ctx context.Context, chatID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup, customReplies map[string]string) {
	e.sendTemplatedMessage(ctx, chatID, "welcome_new_user", text, keyboard, customReplies)
}

func (e *BotEngine) handleCallbackQuery(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	data := cb.Data

	// Acknowledge callback query
	callbackResp := tgbotapi.NewCallback(cb.ID, "")
	_, _ = e.bot.Request(callbackResp)

	// Ban guard: if user is banned by admin, block actions
	if userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID); err == nil && userRes != nil {
		if userRes.User.IsBanned.Valid && userRes.User.IsBanned.Bool {
			t := i18n.GetBundle(e.lang)
			reason := "Suspended by admin"
			if userRes.User.BanReason.Valid && userRes.User.BanReason.String != "" {
				reason = userRes.User.BanReason.String
			}
			e.sendMessage(chatID, fmt.Sprintf(t.BannedMessage, reason), nil)
			return
		}
	}

	if strings.HasPrefix(data, "action:claim_trial") {
		parts := strings.Split(data, ":")
		refCode := ""
		if len(parts) > 2 {
			refCode = parts[2]
		}
		e.handleClaimTrial(ctx, chatID, cb.From.UserName, refCode)
		return
	}

	if data == "action:status" || data == "status" {
		e.handleStatus(ctx, chatID)
		return
	}

	if data == "action:buy" || data == "action:plans" || data == "plans" || data == "buy" {
		e.handleBuy(ctx, chatID)
		return
	}

	if data == "action:reset_keys" {
		e.handleResetPrompt(ctx, chatID)
		return
	}

	if data == "action:confirm_reset_keys" {
		e.handleConfirmResetKeys(ctx, chatID)
		return
	}

	if data == "action:cancel_reset" {
		e.handleStatus(ctx, chatID)
		return
	}

	if data == "action:devices" || data == "devices" {
		e.handleDeviceWizard(ctx, chatID)
		return
	}

	if strings.HasPrefix(data, "action:device:") {
		platform := strings.TrimPrefix(data, "action:device:")
		e.handlePlatformGuide(ctx, chatID, platform)
		return
	}

	if strings.HasPrefix(data, "device:") {
		platform := strings.TrimPrefix(data, "device:")
		e.handlePlatformGuide(ctx, chatID, platform)
		return
	}

	if data == "action:nodes" || data == "action:servers" || data == "servers" {
		e.handleServerList(ctx, chatID)
		return
	}

	if data == "action:promo" {
		t := i18n.GetBundle(e.lang)
		e.userStates[chatID] = "awaiting_promo"
		e.sendMessage(chatID, t.PromoPrompt, nil)
		return
	}

	if data == "action:email" || data == "action:link_email" {
		t := i18n.GetBundle(e.lang)
		e.userStates[chatID] = "awaiting_email"
		e.sendMessage(chatID, t.EmailPrompt, nil)
		return
	}

	if data == "action:restore" || data == "action:restore_account" {
		t := i18n.GetBundle(e.lang)
		e.userStates[chatID] = "awaiting_restore"
		e.sendMessage(chatID, t.RestorePrompt, nil)
		return
	}

	if data == "action:referral" || data == "referral" {
		e.handleReferral(ctx, chatID)
		return
	}

	if data == "action:help" || data == "help" {
		e.handleHelp(ctx, chatID)
		return
	}

	if data == "action:support" || data == "support" {
		e.handleSupport(ctx, chatID)
		return
	}

	if data == "action:back_main" || data == "back_main" {
		e.handleStart(ctx, cb.Message, "")
		return
	}

	if strings.HasPrefix(data, "plan:") {
		// Plan selected -> show duration
		planID := strings.TrimPrefix(data, "plan:")
		e.handleSelectDuration(ctx, chatID, planID)
		return
	}

	if strings.HasPrefix(data, "dur:") {
		// Duration selected (dur:planID:months)
		parts := strings.Split(data, ":")
		if len(parts) == 3 {
			planID := parts[1]
			months, _ := strconv.Atoi(parts[2])
			e.handleSelectPayment(ctx, chatID, planID, int32(months))
		}
		return
	}

	if strings.HasPrefix(data, "pay:") {
		// Gateway selected (pay:planID:months:gateway)
		parts := strings.Split(data, ":")
		if len(parts) == 4 {
			planID := parts[1]
			months, _ := strconv.Atoi(parts[2])
			gateway := parts[3]
			e.handleCheckout(ctx, chatID, planID, int32(months), gateway)
		}
		return
	}

	if strings.HasPrefix(data, "action:qr:") {
		token := strings.TrimPrefix(data, "action:qr:")
		e.sendQRCode(chatID, token)
		return
	}
}

func (e *BotEngine) handleClaimTrial(ctx context.Context, chatID int64, username, refCode string) {
	t := i18n.GetBundle(e.lang)

	replies, _ := e.cpClient.GetBotReplies(ctx)
	if replies != nil && replies["email_policy"] == "required" {
		userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
		if err != nil || userRes == nil || !userRes.User.Email.Valid || userRes.User.Email.String == "" || strings.HasSuffix(userRes.User.Email.String, "@t.me") {
			e.userStates[chatID] = "awaiting_email"
			e.sendMessage(chatID, t.EmailRequiredNotice, nil)
			return
		}
	}

	trialRes, err := e.cpClient.CreateTrial(ctx, chatID, username, refCode)
	if err != nil {
		e.sendMessage(chatID, fmt.Sprintf("Failed to activate free trial: %v", err), nil)
		return
	}

	trialTpl := t.TrialActivated
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["trial_activated"]; ok && val != "" {
			trialTpl = val
		}
	}

	text := fmt.Sprintf(trialTpl,
		i18n.FormatBytes(trialRes.TrafficLimitBytes),
		trialRes.TrialHours,
		trialRes.SubscriptionURL,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Get QR Code", fmt.Sprintf("action:qr:%s", trialRes.SubscriptionToken)),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnHelp, "action:help"),
		),
	)

	e.sendTemplatedMessage(ctx, chatID, "trial_activated", text, &keyboard, customReplies)
}

func (e *BotEngine) handleStatus(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil || userRes == nil {
		e.sendMessage(chatID, "No active subscription found. Tap /start to activate your account.", nil)
		return
	}

	usernameStr := userRes.User.Username
	if usernameStr == "" {
		usernameStr = strconv.FormatInt(chatID, 10)
	}

	expiresStr := "Unlimited"
	if e.lang == i18n.RU {
		expiresStr = "Бессрочно"
	}
	if userRes.User.ExpiresAt.Valid && !userRes.User.ExpiresAt.Time.IsZero() {
		expiresStr = userRes.User.ExpiresAt.Time.Format("2006-01-02 15:04")
	}

	baseURL := e.getPublicBaseURL()
	fullSubURL := userRes.SubscriptionURL
	if strings.HasPrefix(fullSubURL, "/") {
		fullSubURL = baseURL + fullSubURL
	}

	planName := "Custom Tier"
	text := fmt.Sprintf(t.SubscriptionInfo,
		userRes.User.ID.String()[:8],
		usernameStr,
		planName,
		i18n.FormatBytes(userRes.User.TrafficUsed.Int64),
		i18n.FormatBytes(userRes.User.TrafficLimit.Int64),
		expiresStr,
		fullSubURL,
	)

	portalURL := fmt.Sprintf("%s/client/%s", baseURL, userRes.SubscriptionToken)

	referralEnabled := true
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil && customReplies != nil {
		if customReplies["referral_enabled"] == "false" {
			referralEnabled = false
		}
	}

	var statusBottomRow []tgbotapi.InlineKeyboardButton
	statusBottomRow = append(statusBottomRow, tgbotapi.NewInlineKeyboardButtonData(t.BtnNodes, "action:nodes"))
	if referralEnabled {
		statusBottomRow = append(statusBottomRow, tgbotapi.NewInlineKeyboardButtonData(t.BtnReferral, "action:referral"))
	}

	rows := [][]tgbotapi.InlineKeyboardButton{
		{tgbotapi.NewInlineKeyboardButtonURL(t.BtnOpenPortal, portalURL)},
		{
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDeviceWizard, "action:devices"),
			tgbotapi.NewInlineKeyboardButtonData("Get QR Code", fmt.Sprintf("action:qr:%s", userRes.SubscriptionToken)),
		},
		{
			tgbotapi.NewInlineKeyboardButtonData(t.BtnResetKeys, "action:reset_keys"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
		},
		statusBottomRow,
	}

	if !userRes.User.Email.Valid || userRes.User.Email.String == "" || strings.HasSuffix(userRes.User.Email.String, "@t.me") {
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(t.BtnLinkEmail, "action:email"),
		})
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	e.sendMessage(chatID, text, &keyboard)
}

func (e *BotEngine) handleReferral(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil && customReplies["referral_enabled"] == "false" {
		msg := "The referral program is currently paused."
		if e.lang == i18n.RU {
			msg = "Реферальная программа временно приостановлена."
		}
		e.sendMessage(chatID, msg, nil)
		return
	}

	stats, err := e.cpClient.GetReferralStats(ctx, chatID)
	refCode := fmt.Sprintf("ref_%d", chatID)
	var refCount int64
	if err == nil && stats != nil {
		if stats.ReferralCode != "" {
			refCode = stats.ReferralCode
		}
		refCount = stats.ReferralCount
	}

	botUsername := "SimpleVPNBot"
	if e.bot != nil && e.bot.Self.UserName != "" {
		botUsername = e.bot.Self.UserName
	}
	refLink := fmt.Sprintf("https://t.me/%s?start=%s", botUsername, refCode)
	shareURL := fmt.Sprintf("https://t.me/share/url?url=%s&text=%s",
		url.QueryEscape(refLink),
		url.QueryEscape("Connect to high-speed uncapped VPN access:"),
	)

	text := fmt.Sprintf(t.ReferralInfo, refLink, refCount)
	if customReplies != nil {
		if val, ok := customReplies["referral_overview"]; ok && val != "" {
			bonusDays := int64(7)
			if stats != nil && stats.BonusDaysPerReferral > 0 {
				bonusDays = int64(stats.BonusDaysPerReferral) * refCount
			}
			val = strings.ReplaceAll(val, "{refprocent}", "15")
			if strings.Count(val, "%") == 3 {
				text = fmt.Sprintf(val, refLink, refCount, bonusDays)
			} else if strings.Count(val, "%") == 2 {
				text = fmt.Sprintf(val, refLink, refCount)
			} else if strings.Count(val, "%") == 1 {
				text = fmt.Sprintf(val, refLink)
			} else if !strings.Contains(val, "%") {
				text = fmt.Sprintf("%s\n\n%s", val, refLink)
			}
		}
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(t.BtnShareReferral, shareURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
		),
	)

	e.sendTemplatedMessage(ctx, chatID, "referral_overview", text, &keyboard, customReplies)
}

func (e *BotEngine) handleBuy(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	replies, _ := e.cpClient.GetBotReplies(ctx)
	if replies != nil && replies["email_policy"] == "required" {
		userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
		if err != nil || userRes == nil || !userRes.User.Email.Valid || userRes.User.Email.String == "" || strings.HasSuffix(userRes.User.Email.String, "@t.me") {
			e.userStates[chatID] = "awaiting_email"
			e.sendMessage(chatID, t.EmailRequiredNotice, nil)
			return
		}
	}

	plans, err := e.cpClient.ListPlans(ctx)
	if err != nil || len(plans) == 0 {
		e.sendMessage(chatID, "No commercial plans are currently configured. Please check back soon.", nil)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🎁 Free Trial (3 Days) - $0", "action:trial"),
	))
	for _, p := range plans {
		if p.IsTrial.Bool {
			continue // Skip trial tier from purchase list
		}
		btnText := fmt.Sprintf("⚡ %s - $%s/mo", p.Name, p.Price())
		if p.PriceStars.Valid && p.PriceStars.Int32 > 0 {
			btnText += fmt.Sprintf(" (⭐️ %d)", p.PriceStars.Int32)
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnText, fmt.Sprintf("plan:%s", p.ID.String())),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("<< "+t.BtnBack, "action:start"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	catalogHeader := t.SelectPlan
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["catalog_header"]; ok && val != "" {
			catalogHeader = val
		}
	}
	e.sendTemplatedMessage(ctx, chatID, "catalog_header", catalogHeader, &keyboard, customReplies)
}

func (e *BotEngine) handleSelectDuration(ctx context.Context, chatID int64, planID string) {
	t := i18n.GetBundle(e.lang)

	planName := "Plan"
	if plans, err := e.cpClient.ListPlans(ctx); err == nil {
		for _, p := range plans {
			if p.ID.String() == planID {
				planName = p.Name
				break
			}
		}
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDuration1m, fmt.Sprintf("dur:%s:1", planID)),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDuration3m, fmt.Sprintf("dur:%s:3", planID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDuration6m, fmt.Sprintf("dur:%s:6", planID)),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDuration12m, fmt.Sprintf("dur:%s:12", planID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnBack, "action:buy"),
		),
	)

	e.sendMessage(chatID, fmt.Sprintf(t.SelectDuration, planName), &keyboard)
}

func (e *BotEngine) handleSelectPayment(ctx context.Context, chatID int64, planID string, months int32) {
	t := i18n.GetBundle(e.lang)

	settings, err := e.cpClient.GetBillingSettings(ctx)
	if err == nil && settings != nil {
		if settings.CryptobotApiToken != "" && e.paymentMgr != nil && e.paymentMgr.Crypto() != nil {
			e.paymentMgr.Crypto().SetAPIToken(settings.CryptobotApiToken)
		}
	}

	starsEnabled := true
	cryptoEnabled := true
	if settings != nil {
		starsEnabled = settings.TelegramStarsEnabled
		cryptoEnabled = settings.CryptobotEnabled
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	if starsEnabled {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Telegram Stars (In-App)", fmt.Sprintf("pay:%s:%d:stars", planID, months)),
		))
	}
	if cryptoEnabled {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("CryptoBot (USDT, TON, BTC)", fmt.Sprintf("pay:%s:%d:cryptobot", planID, months)),
		))
	}

	if len(rows) == 0 {
		e.sendMessage(chatID, "Payment gateways are temporarily disabled. Please check back later.", nil)
		return
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(t.BtnBack, fmt.Sprintf("plan:%s", planID)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	selectPayment := t.SelectPayment
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["payment_method_prompt"]; ok && val != "" {
			selectPayment = val
		}
	}
	e.sendTemplatedMessage(ctx, chatID, "payment_method_prompt", selectPayment, &keyboard, customReplies)
}

func (e *BotEngine) handleCheckout(ctx context.Context, chatID int64, planIDStr string, months int32, gateway string) {
	t := i18n.GetBundle(e.lang)

	settings, _ := e.cpClient.GetBillingSettings(ctx)
	if settings != nil {
		if gateway == "cryptobot" {
			if !settings.CryptobotEnabled {
				e.sendMessage(chatID, "CryptoBot payment gateway is currently disabled. Please choose another payment method.", nil)
				return
			}
			if settings.CryptobotApiToken != "" && e.paymentMgr != nil && e.paymentMgr.Crypto() != nil {
				e.paymentMgr.Crypto().SetAPIToken(settings.CryptobotApiToken)
			}
		} else if gateway == "stars" {
			if !settings.TelegramStarsEnabled {
				e.sendMessage(chatID, "Telegram Stars payment gateway is currently disabled. Please choose another payment method.", nil)
				return
			}
		}
	}

	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil || userRes == nil {
		e.sendMessage(chatID, "Please initialize your account with /start first.", nil)
		return
	}

	planUUID, err := uuid.Parse(planIDStr)
	if err != nil {
		e.sendMessage(chatID, "Invalid plan selected.", nil)
		return
	}

	inv, err := e.cpClient.CreateInvoice(ctx, client.CreateInvoiceRequest{
		UserID:         userRes.User.ID,
		PlanID:         planUUID,
		Gateway:        gateway,
		DurationMonths: months,
	})

	if err != nil {
		e.sendMessage(chatID, fmt.Sprintf("Failed to generate invoice: %v", err), nil)
		return
	}

	if gateway == "stars" {
		starsAmount, _ := strconv.Atoi(inv.Amount)
		if starsAmount <= 0 {
			e.sendMessage(chatID, "Payment with Telegram Stars is not configured for this plan. Please select CryptoBot or choose another plan.", nil)
			return
		}
		if e.paymentMgr != nil && e.paymentMgr.Stars() != nil {
			_, errStars := e.paymentMgr.Stars().SendStarsInvoice(
				chatID,
				fmt.Sprintf("VPN Access (%d months)", months),
				"Uncapped censorship-resistant VPN access",
				inv.OrderID.String(),
				starsAmount,
			)
			if errStars != nil {
				e.sendMessage(chatID, fmt.Sprintf("Failed to create Stars invoice: %v", errStars), nil)
			}
		}
		return
	}

	// External payment URL (CryptoBot, AAIO, etc.)
	payURL := inv.CheckoutURL
	if gateway == "cryptobot" && e.paymentMgr != nil && e.paymentMgr.Crypto() != nil {
		cryptoInv, errCrypto := e.paymentMgr.Crypto().CreateInvoice(ctx, payment.CryptoBotCreateInvoiceParams{
			Amount:      inv.Amount,
			Asset:       "USDT",
			Description: fmt.Sprintf("VPN %d months (order %s)", months, inv.OrderID.String()[:8]),
			Payload:     inv.OrderID.String(),
		})
		if errCrypto == nil && cryptoInv != nil && cryptoInv.PayURL != "" {
			payURL = cryptoInv.PayURL
		}
	}
	if payURL == "" {
		payURL = fmt.Sprintf("https://t.me/CryptoBot?start=%s", inv.ExternalInvoiceID)
	}

	text := fmt.Sprintf(t.InvoiceCreated, inv.OrderID.String()[:8], inv.Amount, inv.Currency, strings.ToUpper(gateway))
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["invoice_created"]; ok && val != "" {
			if strings.Count(val, "%s") == 4 {
				text = fmt.Sprintf(val, inv.OrderID.String()[:8], inv.Amount, inv.Currency, strings.ToUpper(gateway))
			} else if strings.Count(val, "%s") >= 1 {
				text = fmt.Sprintf(val, inv.OrderID.String()[:8])
			} else {
				text = val
			}
		}
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL("Pay Now", payURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
		),
	)

	e.sendTemplatedMessage(ctx, chatID, "invoice_created", text, &keyboard, customReplies)
}

func (e *BotEngine) handleResetPrompt(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil || userRes == nil {
		e.sendMessage(chatID, "No active subscription found. Tap /start to begin.", nil)
		return
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnConfirmReset, "action:confirm_reset_keys"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnCancelReset, "action:cancel_reset"),
		),
	)

	prompt := t.ResetConfirmPrompt
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["keys_reset_prompt"]; ok && val != "" {
			prompt = val
		}
	}
	e.sendTemplatedMessage(ctx, chatID, "keys_reset_prompt", prompt, &keyboard, customReplies)
}

func (e *BotEngine) handleConfirmResetKeys(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil || userRes == nil {
		e.sendMessage(chatID, "No active subscription found. Tap /start to begin.", nil)
		return
	}

	rotated, err := e.cpClient.RotateKeys(ctx, userRes.User.ID)
	if err != nil {
		e.sendMessage(chatID, fmt.Sprintf("Failed to rotate keys: %v", err), nil)
		return
	}

	baseURL := e.getPublicBaseURL()
	fullSubURL := rotated.SubscriptionURL
	if strings.HasPrefix(fullSubURL, "/") {
		fullSubURL = baseURL + fullSubURL
	}
	portalURL := fmt.Sprintf("%s/client/%s", baseURL, rotated.SubscriptionToken)

	// Fetch updated configuration file (try amneziawg first, then wireguard)
	var configBytes []byte
	configName := "amneziawg.conf"
	cfg, cfgErr := e.cpClient.GetSubscriptionConfig(ctx, rotated.SubscriptionToken, "amneziawg")
	if cfgErr == nil && len(cfg) > 0 {
		configBytes = cfg
	} else {
		cfgWg, cfgWgErr := e.cpClient.GetSubscriptionConfig(ctx, rotated.SubscriptionToken, "wireguard")
		if cfgWgErr == nil && len(cfgWg) > 0 {
			configBytes = cfgWg
			configName = "wireguard.conf"
		}
	}

	// Send updated configuration file to user
	if len(configBytes) > 0 {
		doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
			Name:  configName,
			Bytes: configBytes,
		})
		doc.Caption = fmt.Sprintf("Updated VPN Configuration (%s)", configName)
		_, _ = e.bot.Send(doc)
	}

	resetSuccessTpl := t.KeysResetSuccess
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["keys_reset_success"]; ok && val != "" {
			resetSuccessTpl = val
		}
	}

	var text string
	if strings.Contains(resetSuccessTpl, "%s") {
		text = fmt.Sprintf(resetSuccessTpl, fullSubURL)
	} else {
		text = fmt.Sprintf("%s\n\n%s", resetSuccessTpl, fullSubURL)
	}
	text += fmt.Sprintf("\n\nWeb Portal:\n%s", portalURL)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDeviceWizard, "action:devices"),
			tgbotapi.NewInlineKeyboardButtonData("Get QR Code", fmt.Sprintf("action:qr:%s", rotated.SubscriptionToken)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
		),
	)

	e.sendTemplatedMessage(ctx, chatID, "keys_reset_success", text, &keyboard, customReplies)
}


func (e *BotEngine) handleDeviceWizard(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("iOS (iPhone / iPad)", "device:ios"),
			tgbotapi.NewInlineKeyboardButtonData("Android", "device:android"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Windows", "device:windows"),
			tgbotapi.NewInlineKeyboardButtonData("macOS", "device:macos"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Linux", "device:linux"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnBack, "action:status"),
		),
	)

	e.sendMessage(chatID, t.DeviceWizardTitle, &keyboard)
}

func (e *BotEngine) handlePlatformGuide(ctx context.Context, chatID int64, platform string) {
	t := i18n.GetBundle(e.lang)

	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil || userRes == nil {
		e.sendMessage(chatID, "No active subscription found. Tap /start to activate your account.", nil)
		return
	}

	baseURL := e.cpClient.BaseURL()
	if baseURL == "" {
		baseURL = "http://localhost:8110"
	}
	fullSubURL := userRes.SubscriptionURL
	if strings.HasPrefix(fullSubURL, "/") {
		fullSubURL = baseURL + fullSubURL
	}
	encodedSubURL := url.QueryEscape(fullSubURL)
	token := userRes.SubscriptionToken

	var guideText string
	var appDownloadURL string
	var appButtonText string
	switch strings.ToLower(platform) {
	case "ios":
		appDownloadURL = "https://apps.apple.com/app/happ-proxy-utility/id6504287215"
		appButtonText = "App Store (Happ)"
		guideText = fmt.Sprintf("Setup Guide for iOS (iPhone / iPad):\n\n1. Install Happ from the App Store.\n2. Tap '%s' below or open deep link:\n`happ://add/%s`\n3. Allow VPN profile when prompted and toggle switch to Connected.\n\nAlternative client: Streisand (`streisand://import/%s`)", t.BtnConnectOneClick, encodedSubURL, encodedSubURL)

	case "android":
		appDownloadURL = "https://play.google.com/store/apps/details?id=com.v2ray.ang"
		appButtonText = "Google Play (v2rayNG)"
		guideText = fmt.Sprintf("Setup Guide for Android:\n\n1. Install v2rayNG from Google Play or GitHub.\n2. Tap '%s' below or open deep link:\n`v2rayng://install-config?url=%s`\n3. Tap the round V icon at the bottom to connect.\n\nAlternative: Sing-box (`sing-box://import-remote-profile?url=%s#Simple-VPN`)", t.BtnConnectOneClick, encodedSubURL, encodedSubURL)

	case "windows":
		appDownloadURL = "https://github.com/clash-verge-rev/clash-verge-rev/releases"
		appButtonText = "GitHub (Clash Verge Rev)"
		guideText = fmt.Sprintf("Setup Guide for Windows:\n\n1. Download and install Clash Verge Rev from GitHub Releases.\n2. Tap '%s' below or open deep link:\n`clash://install-config?url=%s&name=Simple-VPN`\n3. Turn on System Proxy and TUN Mode in program settings.", t.BtnConnectOneClick, encodedSubURL)

	case "macos":
		appDownloadURL = "https://apps.apple.com/app/streisand/id6450534064"
		appButtonText = "Mac App Store (Streisand)"
		guideText = fmt.Sprintf("Setup Guide for macOS:\n\n1. Install Streisand from Mac App Store.\n2. Tap '%s' below or open deep link:\n`streisand://import/%s`\n3. Select node and switch Connection to Connected.", t.BtnConnectOneClick, encodedSubURL)

	case "linux":
		appDownloadURL = "https://github.com/SagerNet/sing-box/releases"
		appButtonText = "GitHub (Sing-box)"
		guideText = fmt.Sprintf("Setup Guide for Linux:\n\n1. Download sing-box or Hiddify from GitHub Releases.\n2. Tap '%s' below or open deep link:\n`sing-box://import-remote-profile?url=%s#Simple-VPN`\n3. Run sing-box daemon or Hiddify GUI application.", t.BtnConnectOneClick, encodedSubURL)

	default:
		e.handleDeviceWizard(ctx, chatID)
		return
	}

	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		guideKey := "setup_guide_" + strings.ToLower(platform)
		if val, ok := customReplies[guideKey]; ok && val != "" {
			switch strings.Count(val, "%s") {
			case 3:
				guideText = fmt.Sprintf(val, t.BtnConnectOneClick, encodedSubURL, encodedSubURL)
			case 2:
				guideText = fmt.Sprintf(val, t.BtnConnectOneClick, encodedSubURL)
			case 1:
				guideText = fmt.Sprintf(val, encodedSubURL)
			case 0:
				guideText = val
			}
		}
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📷 Get QR Code", "action:qr:"+token),
		tgbotapi.NewInlineKeyboardButtonURL("📥 "+appButtonText, appDownloadURL),
	))
	if customReplies != nil {
		if chLink := customReplies["channel_link"]; chLink != "" {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("📖 Video Guide & Updates", chLink),
			))
		}
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("<< "+t.BtnDeviceWizard, "action:devices"),
		tgbotapi.NewInlineKeyboardButtonData("📊 "+t.BtnStatus, "action:status"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	guideKey := "setup_guide_" + strings.ToLower(platform)
	e.sendTemplatedMessage(ctx, chatID, guideKey, guideText, &keyboard, customReplies)

	// Send downloadable .conf profile for desktop platforms (Windows, macOS, Linux)
	platLower := strings.ToLower(platform)
	if platLower == "windows" || platLower == "macos" || platLower == "linux" {
		cfgBytes, err := e.cpClient.GetSubscriptionConfig(ctx, token, "amneziawg")
		cfgName := "amneziawg.conf"
		if err != nil || len(cfgBytes) == 0 {
			cfgBytes, _ = e.cpClient.GetSubscriptionConfig(ctx, token, "wireguard")
			cfgName = "wireguard.conf"
		}
		if len(cfgBytes) > 0 {
			doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
				Name:  cfgName,
				Bytes: cfgBytes,
			})
			doc.Caption = fmt.Sprintf("Configuration File (%s) for %s / Router", cfgName, strings.ToUpper(platform))
			_, _ = e.bot.Send(doc)
		}
	}
}

func (e *BotEngine) handleServerList(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	nodes, err := e.cpClient.ListActiveNodes(ctx)
	if err != nil || len(nodes) == 0 {
		e.sendMessage(chatID, "Edge network nodes are currently undergoing maintenance.", nil)
		return
	}

	var sb strings.Builder
	for _, n := range nodes {
		region := "Global"
		if n.Region.Valid {
			region = n.Region.String
		}
		status := "Online"
		if n.Status.Valid {
			status = strings.ToUpper(n.Status.String)
		}
		sb.WriteString(fmt.Sprintf("- %s [%s] - %s (Capacity: %d Gbps)\n", n.Name, region, status, n.CapacityGbps.Int32))
	}

	text := fmt.Sprintf(t.NodeListHeader, sb.String())
	e.sendMessage(chatID, text, nil)
}

func (e *BotEngine) processPromoCode(ctx context.Context, chatID int64, code string) {
	t := i18n.GetBundle(e.lang)

	promo, err := e.cpClient.ValidatePromo(ctx, code)
	if err != nil || promo == nil {
		e.sendMessage(chatID, t.PromoInvalid, nil)
		return
	}

	e.sendMessage(chatID, fmt.Sprintf(t.PromoSuccess, promo.Code), nil)
}

func (e *BotEngine) processLinkEmail(ctx context.Context, chatID int64, email string) {
	t := i18n.GetBundle(e.lang)
	email = strings.TrimSpace(strings.ToLower(email))
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") || len(email) < 5 {
		e.sendMessage(chatID, t.EmailInvalid, nil)
		return
	}

	replies, _ := e.cpClient.GetBotReplies(ctx)
	if replies != nil && replies["email_policy"] == "disabled" {
		e.sendMessage(chatID, t.EmailPolicyDisabled, nil)
		return
	}

	otpEnabled := false
	if replies != nil && replies["email_otp_enabled"] == "true" {
		otpEnabled = true
	}

	if otpEnabled {
		otpRes, err := e.cpClient.RequestEmailOTP(ctx, chatID, email, "link_email")
		if err != nil {
			if strings.Contains(err.Error(), "already linked") || strings.Contains(err.Error(), "conflict") {
				e.sendMessage(chatID, t.EmailAlreadyLinked, nil)
				return
			}
			e.sendMessage(chatID, fmt.Sprintf("Failed to request verification code: %v", err), nil)
			return
		}
		e.userStates[chatID] = "awaiting_email_otp"
		e.userPendingEmail[chatID] = email

		prompt := fmt.Sprintf(t.OTPPrompt, email)
		if otpRes != nil && otpRes.Simulated && otpRes.Code != "" {
			prompt += fmt.Sprintf("\n\n[Dev Mode Code: %s]", otpRes.Code)
		}
		e.sendMessage(chatID, prompt, nil)
		return
	}

	_, err := e.cpClient.LinkEmail(ctx, chatID, email)
	if err != nil {
		if strings.Contains(err.Error(), "already linked") || strings.Contains(err.Error(), "conflict") {
			e.sendMessage(chatID, t.EmailAlreadyLinked, nil)
			return
		}
		e.sendMessage(chatID, fmt.Sprintf("Failed to link email: %v", err), nil)
		return
	}

	e.sendMessage(chatID, fmt.Sprintf(t.EmailLinkedSuccess, email), nil)
}

func (e *BotEngine) processVerifyEmailOTP(ctx context.Context, chatID int64, otp string, from *tgbotapi.User) {
	t := i18n.GetBundle(e.lang)
	otp = strings.TrimSpace(otp)
	email := e.userPendingEmail[chatID]
	if email == "" {
		delete(e.userStates, chatID)
		e.sendMessage(chatID, t.OTPExpired, nil)
		return
	}

	username, firstName, lastName := "", "", ""
	if from != nil {
		username = from.UserName
		firstName = from.FirstName
		lastName = from.LastName
	}

	_, err := e.cpClient.VerifyEmailOTP(ctx, chatID, email, otp, "link_email", username, firstName, lastName)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "expired") || strings.Contains(errStr, "exhausted") || strings.Contains(errStr, "not found") {
			delete(e.userStates, chatID)
			delete(e.userPendingEmail, chatID)
			e.sendMessage(chatID, t.OTPExpired, nil)
			return
		}
		if strings.Contains(errStr, "invalid") {
			e.sendMessage(chatID, fmt.Sprintf(t.OTPInvalid, 3), nil)
			return
		}
		e.sendMessage(chatID, fmt.Sprintf("Verification failed: %v", err), nil)
		return
	}

	delete(e.userStates, chatID)
	delete(e.userPendingEmail, chatID)
	e.sendMessage(chatID, fmt.Sprintf(t.EmailLinkedSuccess, email), nil)
}

func (e *BotEngine) processRestoreAccount(ctx context.Context, chatID int64, from *tgbotapi.User, email string) {
	t := i18n.GetBundle(e.lang)
	email = strings.TrimSpace(strings.ToLower(email))
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") || len(email) < 5 {
		e.sendMessage(chatID, t.EmailInvalid, nil)
		return
	}

	replies, _ := e.cpClient.GetBotReplies(ctx)
	otpEnabled := false
	if replies != nil && replies["email_otp_enabled"] == "true" {
		otpEnabled = true
	}

	username, firstName, lastName := "", "", ""
	if from != nil {
		username = from.UserName
		firstName = from.FirstName
		lastName = from.LastName
	}

	if otpEnabled {
		otpRes, err := e.cpClient.RequestEmailOTP(ctx, chatID, email, "restore_account")
		if err != nil {
			if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
				e.sendMessage(chatID, t.RestoreNotFound, nil)
				return
			}
			e.sendMessage(chatID, fmt.Sprintf("Failed to request verification code: %v", err), nil)
			return
		}
		e.userStates[chatID] = "awaiting_restore_otp"
		e.userPendingEmail[chatID] = email

		prompt := fmt.Sprintf(t.OTPPrompt, email)
		if otpRes != nil && otpRes.Simulated && otpRes.Code != "" {
			prompt += fmt.Sprintf("\n\n[Dev Mode Code: %s]", otpRes.Code)
		}
		e.sendMessage(chatID, prompt, nil)
		return
	}

	res, err := e.cpClient.RestoreAccount(ctx, email, chatID, username, firstName, lastName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			e.sendMessage(chatID, t.RestoreNotFound, nil)
			return
		}
		e.sendMessage(chatID, fmt.Sprintf("Failed to restore account: %v", err), nil)
		return
	}

	baseURL := e.getPublicBaseURL()
	fullSubURL := res.SubscriptionURL
	if strings.HasPrefix(fullSubURL, "/") {
		fullSubURL = baseURL + fullSubURL
	}

	text := fmt.Sprintf(t.RestoreSuccess, fullSubURL)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDeviceWizard, "action:devices"),
		),
	)
	e.sendMessage(chatID, text, &keyboard)
}

func (e *BotEngine) processVerifyRestoreOTP(ctx context.Context, chatID int64, otp string, from *tgbotapi.User) {
	t := i18n.GetBundle(e.lang)
	otp = strings.TrimSpace(otp)
	email := e.userPendingEmail[chatID]
	if email == "" {
		delete(e.userStates, chatID)
		e.sendMessage(chatID, t.OTPExpired, nil)
		return
	}

	username, firstName, lastName := "", "", ""
	if from != nil {
		username = from.UserName
		firstName = from.FirstName
		lastName = from.LastName
	}

	res, err := e.cpClient.VerifyEmailOTP(ctx, chatID, email, otp, "restore_account", username, firstName, lastName)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "expired") || strings.Contains(errStr, "exhausted") || strings.Contains(errStr, "not found") {
			delete(e.userStates, chatID)
			delete(e.userPendingEmail, chatID)
			e.sendMessage(chatID, t.OTPExpired, nil)
			return
		}
		if strings.Contains(errStr, "invalid") {
			e.sendMessage(chatID, fmt.Sprintf(t.OTPInvalid, 3), nil)
			return
		}
		e.sendMessage(chatID, fmt.Sprintf("Verification failed: %v", err), nil)
		return
	}

	delete(e.userStates, chatID)
	delete(e.userPendingEmail, chatID)

	baseURL := e.getPublicBaseURL()
	fullSubURL := res.SubscriptionURL
	if strings.HasPrefix(fullSubURL, "/") {
		fullSubURL = baseURL + fullSubURL
	}

	text := fmt.Sprintf(t.RestoreSuccess, fullSubURL)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDeviceWizard, "action:devices"),
		),
	)
	e.sendMessage(chatID, text, &keyboard)
}

func (e *BotEngine) sendQRCode(chatID int64, token string) {
	baseURL := e.getPublicBaseURL()
	fullSubURL := fmt.Sprintf("%s/sub/%s", baseURL, token)
	pngBytes, err := qrcode.Encode(fullSubURL, qrcode.Medium, 256)
	if err != nil {
		e.sendMessage(chatID, "Failed to render QR code.", nil)
		return
	}

	photoFile := tgbotapi.FileBytes{
		Name:  "subscription_qr.png",
		Bytes: pngBytes,
	}

	msg := tgbotapi.NewPhoto(chatID, photoFile)
	msg.Caption = fmt.Sprintf("Universal Subscription QR Code\nToken: %s\nURL: %s", token[:8], fullSubURL)
	if _, err := e.bot.Send(msg); err != nil {
		slog.Error("Failed to send QR code photo", "chat_id", chatID, "error", err)
	}
}

func (e *BotEngine) handlePreCheckoutQuery(query *tgbotapi.PreCheckoutQuery) {
	// Always approve Telegram Stars pre-checkout queries
	answer := tgbotapi.PreCheckoutConfig{
		OK:                 true,
		PreCheckoutQueryID: query.ID,
	}
	_, _ = e.bot.Request(answer)
}

func (e *BotEngine) handleHelp(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)
	helpText := t.HelpText
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["help_text"]; ok && val != "" {
			helpText = val
		}
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDeviceWizard, "action:devices"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnSupport, "action:support"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
		),
	)
	e.sendTemplatedMessage(ctx, chatID, "help_text", helpText, &keyboard, customReplies)
}

func (e *BotEngine) handleSupport(ctx context.Context, chatID int64) {
	supportText := "Need help or experiencing connection issues?\n\nContact network administration or support team via the link below."
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["support_text"]; ok && val != "" {
			supportText = val
		}
	}
	e.sendTemplatedMessage(ctx, chatID, "support_text", supportText, nil, customReplies)
}

func (e *BotEngine) handleSuccessfulPayment(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	orderIDStr := msg.SuccessfulPayment.InvoicePayload

	successMsg := fmt.Sprintf("Payment confirmed for order %s! Your subscription has been renewed.", orderIDStr[:8])
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["payment_success"]; ok && val != "" {
			if strings.Contains(val, "%s") {
				successMsg = fmt.Sprintf(val, orderIDStr[:8])
			} else {
				successMsg = val
			}
		}
	}

	e.sendTemplatedMessage(ctx, chatID, "payment_success", successMsg, nil, customReplies)
	e.handleStatus(ctx, chatID)
}

func (e *BotEngine) getPublicBaseURL() string {
	if pub := strings.TrimSpace(os.Getenv("PUBLIC_URL")); pub != "" {
		return strings.TrimRight(pub, "/")
	}
	if pub := strings.TrimSpace(os.Getenv("EXTERNAL_URL")); pub != "" {
		return strings.TrimRight(pub, "/")
	}
	if pub := strings.TrimSpace(os.Getenv("APP_URL")); pub != "" {
		return strings.TrimRight(pub, "/")
	}
	baseURL := e.cpClient.BaseURL()
	if baseURL == "" || strings.Contains(baseURL, "control-plane") {
		return "http://127.0.0.1:8110"
	}
	return strings.TrimRight(baseURL, "/")
}

func (e *BotEngine) applyTemplateTags(ctx context.Context, chatID int64, text string) string {
	if !strings.Contains(text, "{") {
		return text
	}

	userRes, _ := e.cpClient.GetUserByTelegramID(ctx, chatID)
	username := ""
	firstName := "User"
	userIDStr := strconv.FormatInt(chatID, 10)
	trafficUsed := "0 MB"
	trafficTotal := "Unlimited"
	trafficLeft := "Unlimited"
	expiresAt := "N/A"
	daysLeft := "0"
	subURL := ""
	portalURL := ""
	planName := "Standard"

	baseURL := e.getPublicBaseURL()

	if userRes != nil {
		if userRes.User.Username != "" {
			username = "@" + userRes.User.Username
		}
		if userRes.User.TrafficUsed.Valid {
			trafficUsed = i18n.FormatBytes(userRes.User.TrafficUsed.Int64)
		}
		if userRes.User.TrafficLimit.Valid && userRes.User.TrafficLimit.Int64 > 0 {
			trafficTotal = i18n.FormatBytes(userRes.User.TrafficLimit.Int64)
			leftBytes := userRes.User.TrafficLimit.Int64 - userRes.User.TrafficUsed.Int64
			if leftBytes < 0 {
				leftBytes = 0
			}
			trafficLeft = i18n.FormatBytes(leftBytes)
		}
		if userRes.User.ExpiresAt.Valid && !userRes.User.ExpiresAt.Time.IsZero() {
			expiresAt = userRes.User.ExpiresAt.Time.Format("2006-01-02 15:04")
			rem := time.Until(userRes.User.ExpiresAt.Time)
			if rem > 0 {
				daysLeft = strconv.Itoa(int(rem.Hours() / 24))
			}
		}
		subURL = userRes.SubscriptionURL
		if strings.HasPrefix(subURL, "/") {
			subURL = baseURL + subURL
		}
		portalURL = fmt.Sprintf("%s/client/%s", baseURL, userRes.SubscriptionToken)
	}

	refCode := fmt.Sprintf("ref_%d", chatID)
	refCount := int64(0)
	refDays := int64(0)
	stats, err := e.cpClient.GetReferralStats(ctx, chatID)
	if err == nil && stats != nil {
		if stats.ReferralCode != "" {
			refCode = stats.ReferralCode
		}
		refCount = stats.ReferralCount
		if stats.BonusDaysPerReferral > 0 {
			refDays = int64(stats.BonusDaysPerReferral) * refCount
		}
	}

	refPercent := "0"
	if customReplies, rErr := e.cpClient.GetBotReplies(ctx); rErr == nil && customReplies != nil {
		if val, ok := customReplies["referral_percent"]; ok && val != "" {
			refPercent = val
		}
	}

	botUsername := "SimpleVPNBot"
	if e.bot != nil && e.bot.Self.UserName != "" {
		botUsername = e.bot.Self.UserName
	}
	refLink := fmt.Sprintf("https://t.me/%s?start=%s", botUsername, refCode)

	r := strings.NewReplacer(
		"{username}", username,
		"{first_name}", firstName,
		"{user_id}", userIDStr,
		"{balance}", "0.00",
		"{currency}", "USD",
		"{refprocent}", refPercent,
		"{ref_link}", refLink,
		"{ref_count}", strconv.FormatInt(refCount, 10),
		"{ref_days}", strconv.FormatInt(refDays, 10),
		"{traffic_used}", trafficUsed,
		"{traffic_total}", trafficTotal,
		"{traffic_left}", trafficLeft,
		"{expires_at}", expiresAt,
		"{days_left}", daysLeft,
		"{sub_url}", subURL,
		"{portal_url}", portalURL,
		"{plan_name}", planName,
	)
	return r.Replace(text)
}

func (e *BotEngine) sendMessage(chatID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	text = e.applyTemplateTags(context.Background(), chatID, text)
	msg := tgbotapi.NewMessage(chatID, text)
	if strings.Contains(text, "<") && strings.Contains(text, ">") {
		msg.ParseMode = tgbotapi.ModeHTML
	}
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	_, err := e.bot.Send(msg)
	if err != nil {
		slog.Error("Failed to send telegram message", "chat_id", chatID, "error", err)
		if msg.ParseMode != "" {
			msg.ParseMode = "" // Retry without HTML in case of malformed tag
			if _, retryErr := e.bot.Send(msg); retryErr == nil {
				return
			}
		}
		if keyboard != nil {
			msg.ReplyMarkup = nil
			if _, retryErr := e.bot.Send(msg); retryErr != nil {
				slog.Error("Failed to send fallback message without keyboard", "chat_id", chatID, "error", retryErr)
			}
		}
	}
}

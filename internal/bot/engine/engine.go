package engine

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/payment"
	"github.com/skip2/go-qrcode"
)

type BotEngine struct {
	bot        *tgbotapi.BotAPI
	cpClient   *client.CPClient
	paymentMgr *payment.Manager
	lang       i18n.Language
	userStates map[int64]string // chat_id -> state (e.g. awaiting_promo)
}

func NewBotEngine(bot *tgbotapi.BotAPI, cpClient *client.CPClient, paymentMgr *payment.Manager) *BotEngine {
	return &BotEngine{
		bot:        bot,
		cpClient:   cpClient,
		paymentMgr: paymentMgr,
		lang:       i18n.EN,
		userStates: make(map[int64]string),
	}
}

func (e *BotEngine) Start(ctx context.Context) error {
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

	// Check if awaiting user state
	if state, ok := e.userStates[chatID]; ok && state == "awaiting_promo" {
		delete(e.userStates, chatID)
		e.processPromoCode(ctx, chatID, text)
		return
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

	switch text {
	case "/status", t.BtnStatus:
		e.handleStatus(ctx, chatID)
	case "/buy", t.BtnRenew:
		e.handleBuy(ctx, chatID)
	case "/reset", t.BtnResetKeys, "Сбросить ключи", "Reset Keys", "Перевыпустить ключи / Сбросить подключение", "Rotate Keys / Reset Connection":
		e.handleResetPrompt(ctx, chatID)
	case "/devices", "/connect", t.BtnDeviceWizard, "Подключение по устройствам", "Device Setup Wizard":
		e.handleDeviceWizard(ctx, chatID)
	case "/servers", t.BtnNodes:
		e.handleServerList(ctx, chatID)
	case "/promo", t.BtnEnterPromo:
		e.userStates[chatID] = "awaiting_promo"
		e.sendMessage(chatID, t.PromoPrompt, nil)
	case "/ref", "/referral", t.BtnReferral, "Реферальная программа", "Referral Program":
		e.handleReferral(ctx, chatID)
	case "/help", t.BtnHelp:
		helpText := t.HelpText
		if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil {
			if val, ok := customReplies["help_text"]; ok && val != "" {
				helpText = val
			}
		}
		e.sendMessage(chatID, helpText, nil)
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
		// Existing subscriber
		if userRes.User.IsBanned.Bool {
			e.sendMessage(chatID, fmt.Sprintf(t.BannedMessage, userRes.User.BanReason.String), nil)
			return
		}

		baseURL := e.cpClient.BaseURL()
		if baseURL == "" {
			baseURL = "http://localhost:8110"
		}
		portalURL := fmt.Sprintf("%s/client/%s", baseURL, userRes.SubscriptionToken)

		referralEnabled := true
		if customReplies != nil && customReplies["referral_enabled"] == "false" {
			referralEnabled = false
		}

		var lastRow []tgbotapi.InlineKeyboardButton
		lastRow = append(lastRow, tgbotapi.NewInlineKeyboardButtonData(t.BtnEnterPromo, "action:promo"))
		if referralEnabled {
			lastRow = append(lastRow, tgbotapi.NewInlineKeyboardButtonData(t.BtnReferral, "action:referral"))
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL(t.BtnOpenPortal, portalURL),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
				tgbotapi.NewInlineKeyboardButtonData(t.BtnDeviceWizard, "action:devices"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.BtnResetKeys, "action:reset_keys"),
				tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.BtnNodes, "action:nodes"),
				tgbotapi.NewInlineKeyboardButtonData(t.BtnHelp, "action:help"),
			),
			lastRow,
		)

		welcomeTpl := t.WelcomeActiveUser
		if val, ok := customReplies["welcome_active_user"]; ok && val != "" {
			welcomeTpl = val
		}

		text := fmt.Sprintf(welcomeTpl,
			i18n.FormatBytes(userRes.User.TrafficUsed.Int64),
			i18n.FormatBytes(userRes.User.TrafficLimit.Int64),
			userRes.User.ExpiresAt.Time.Format("2006-01-02 15:04"),
			userRes.SubscriptionURL,
		)
		e.sendMessage(chatID, text, &keyboard)
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
				tgbotapi.NewInlineKeyboardButtonData(t.BtnGetTrial, fmt.Sprintf("action:claim_trial:%s", refCode)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
				tgbotapi.NewInlineKeyboardButtonData("1-Click Setup Guide", "action:help"),
			),
		)
		e.sendMessage(chatID, welcomeNew, &keyboard)
		return
	}

	// No trial: direct to purchase
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnHelp, "action:help"),
		),
	)
	e.sendMessage(chatID, welcomeNew+"\n\n"+t.NoTrialAvailable, &keyboard)
}

func (e *BotEngine) handleCallbackQuery(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	data := cb.Data

	// Acknowledge callback query
	callbackResp := tgbotapi.NewCallback(cb.ID, "")
	_, _ = e.bot.Request(callbackResp)

	if strings.HasPrefix(data, "action:claim_trial") {
		parts := strings.Split(data, ":")
		refCode := ""
		if len(parts) > 2 {
			refCode = parts[2]
		}
		e.handleClaimTrial(ctx, chatID, cb.From.UserName, refCode)
		return
	}

	if data == "action:status" {
		e.handleStatus(ctx, chatID)
		return
	}

	if data == "action:buy" {
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

	if data == "action:devices" {
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

	if data == "action:nodes" {
		e.handleServerList(ctx, chatID)
		return
	}

	if data == "action:promo" {
		t := i18n.GetBundle(e.lang)
		e.userStates[chatID] = "awaiting_promo"
		e.sendMessage(chatID, t.PromoPrompt, nil)
		return
	}

	if data == "action:referral" {
		e.handleReferral(ctx, chatID)
		return
	}

	if data == "action:help" {
		t := i18n.GetBundle(e.lang)
		helpText := t.HelpText
		if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil {
			if val, ok := customReplies["help_text"]; ok && val != "" {
				helpText = val
			}
		}
		e.sendMessage(chatID, helpText, nil)
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

	trialRes, err := e.cpClient.CreateTrial(ctx, chatID, username, refCode)
	if err != nil {
		e.sendMessage(chatID, fmt.Sprintf("Failed to activate free trial: %v", err), nil)
		return
	}

	trialTpl := t.TrialActivated
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil {
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

	e.sendMessage(chatID, text, &keyboard)
}

func (e *BotEngine) handleStatus(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil || userRes == nil {
		e.sendMessage(chatID, "No active subscription found. Tap /start to activate your account.", nil)
		return
	}

	planName := "Custom Tier"
	text := fmt.Sprintf(t.SubscriptionInfo,
		userRes.User.ID.String()[:8],
		userRes.User.Username,
		planName,
		i18n.FormatBytes(userRes.User.TrafficUsed.Int64),
		i18n.FormatBytes(userRes.User.TrafficLimit.Int64),
		userRes.User.ExpiresAt.Time.Format("2006-01-02 15:04"),
		userRes.SubscriptionURL,
	)

	baseURL := e.cpClient.BaseURL()
	if baseURL == "" {
		baseURL = "http://localhost:8110"
	}
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

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(t.BtnOpenPortal, portalURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnDeviceWizard, "action:devices"),
			tgbotapi.NewInlineKeyboardButtonData("Get QR Code", fmt.Sprintf("action:qr:%s", userRes.SubscriptionToken)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnResetKeys, "action:reset_keys"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
		),
		statusBottomRow,
	)

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

	e.sendMessage(chatID, text, &keyboard)
}

func (e *BotEngine) handleBuy(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	plans, err := e.cpClient.ListPlans(ctx)
	if err != nil || len(plans) == 0 {
		e.sendMessage(chatID, "No commercial plans are currently configured. Please check back soon.", nil)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, p := range plans {
		if p.IsTrial.Bool {
			continue // Skip trial tier from purchase list
		}
		btnText := fmt.Sprintf("%s - $%s/mo", p.Name, p.MonthlyPrice.Int.String())
		if p.PriceStars.Valid && p.PriceStars.Int32 > 0 {
			btnText += fmt.Sprintf(" (%d Stars)", p.PriceStars.Int32)
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(btnText, fmt.Sprintf("plan:%s", p.ID.String())),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	catalogHeader := t.SelectPlan
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil && customReplies != nil {
		if val, ok := customReplies["catalog_header"]; ok && val != "" {
			catalogHeader = val
		}
	}
	e.sendMessage(chatID, catalogHeader, &keyboard)
}

func (e *BotEngine) handleSelectDuration(_ context.Context, chatID int64, planID string) {
	t := i18n.GetBundle(e.lang)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1 Month", fmt.Sprintf("dur:%s:1", planID)),
			tgbotapi.NewInlineKeyboardButtonData("3 Months (Save 10%)", fmt.Sprintf("dur:%s:3", planID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("6 Months (Save 20%)", fmt.Sprintf("dur:%s:6", planID)),
			tgbotapi.NewInlineKeyboardButtonData("12 Months (Save 30%)", fmt.Sprintf("dur:%s:12", planID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnBack, "action:buy"),
		),
	)

	e.sendMessage(chatID, fmt.Sprintf(t.SelectDuration, planID[:8]), &keyboard)
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
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil && customReplies != nil {
		if val, ok := customReplies["payment_method_prompt"]; ok && val != "" {
			selectPayment = val
		}
	}
	e.sendMessage(chatID, selectPayment, &keyboard)
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
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil && customReplies != nil {
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

	e.sendMessage(chatID, text, &keyboard)
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
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil && customReplies != nil {
		if val, ok := customReplies["keys_reset_prompt"]; ok && val != "" {
			prompt = val
		}
	}
	e.sendMessage(chatID, prompt, &keyboard)
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

	baseURL := e.cpClient.BaseURL()
	if baseURL == "" {
		baseURL = "http://localhost:8110"
	}
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
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil && customReplies != nil {
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

	e.sendMessage(chatID, text, &keyboard)
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
	var connectURL string

	switch strings.ToLower(platform) {
	case "ios":
		appDownloadURL = "https://apps.apple.com/app/happ-proxy-utility/id6504287215"
		appButtonText = "App Store (Happ)"
		connectURL = fmt.Sprintf("%s/client/%s/connect?client=happ", baseURL, token)
		if e.lang == i18n.RU {
			guideText = fmt.Sprintf("Инструкция по настройке для iOS (iPhone / iPad):\n\n1. Установите клиент Happ из App Store.\n2. Нажмите кнопку «%s» ниже или используйте deep link:\n`happ://add/%s`\n3. Разрешите добавление VPN-конфигурации и включите переключатель.\n\nАльтернативный клиент: Streisand (`streisand://import/%s`)", t.BtnConnectOneClick, encodedSubURL, encodedSubURL)
		} else {
			guideText = fmt.Sprintf("Setup Guide for iOS (iPhone / iPad):\n\n1. Install Happ from the App Store.\n2. Tap '%s' below or open deep link:\n`happ://add/%s`\n3. Allow VPN profile when prompted and toggle switch to Connected.\n\nAlternative client: Streisand (`streisand://import/%s`)", t.BtnConnectOneClick, encodedSubURL, encodedSubURL)
		}

	case "android":
		appDownloadURL = "https://play.google.com/store/apps/details?id=com.v2ray.ang"
		appButtonText = "Google Play (v2rayNG)"
		connectURL = fmt.Sprintf("%s/client/%s/connect?client=v2rayng", baseURL, token)
		if e.lang == i18n.RU {
			guideText = fmt.Sprintf("Инструкция по настройке для Android:\n\n1. Установите приложение v2rayNG из Google Play или GitHub.\n2. Нажмите «%s» ниже или используйте deep link:\n`v2rayng://install-config?url=%s`\n3. Нажмите круглую кнопку подключения (значок V) внизу экрана.\n\nАльтернатива: Sing-box (`sing-box://import-remote-profile?url=%s#Simple-VPN`)", t.BtnConnectOneClick, encodedSubURL, encodedSubURL)
		} else {
			guideText = fmt.Sprintf("Setup Guide for Android:\n\n1. Install v2rayNG from Google Play or GitHub.\n2. Tap '%s' below or open deep link:\n`v2rayng://install-config?url=%s`\n3. Tap the round V icon at the bottom to connect.\n\nAlternative: Sing-box (`sing-box://import-remote-profile?url=%s#Simple-VPN`)", t.BtnConnectOneClick, encodedSubURL, encodedSubURL)
		}

	case "windows":
		appDownloadURL = "https://github.com/clash-verge-rev/clash-verge-rev/releases"
		appButtonText = "GitHub (Clash Verge Rev)"
		connectURL = fmt.Sprintf("%s/client/%s/connect?client=clash", baseURL, token)
		if e.lang == i18n.RU {
			guideText = fmt.Sprintf("Инструкция по настройке для Windows:\n\n1. Скачайте и установите Clash Verge Rev с GitHub Releases.\n2. Нажмите «%s» ниже или используйте deep link:\n`clash://install-config?url=%s&name=Simple-VPN`\n3. Включите System Proxy и TUN Mode в настройках программы.", t.BtnConnectOneClick, encodedSubURL)
		} else {
			guideText = fmt.Sprintf("Setup Guide for Windows:\n\n1. Download and install Clash Verge Rev from GitHub Releases.\n2. Tap '%s' below or open deep link:\n`clash://install-config?url=%s&name=Simple-VPN`\n3. Turn on System Proxy and TUN Mode in program settings.", t.BtnConnectOneClick, encodedSubURL)
		}

	case "macos":
		appDownloadURL = "https://apps.apple.com/app/streisand/id6450534064"
		appButtonText = "Mac App Store (Streisand)"
		connectURL = fmt.Sprintf("%s/client/%s/connect?client=streisand", baseURL, token)
		if e.lang == i18n.RU {
			guideText = fmt.Sprintf("Инструкция по настройке для macOS:\n\n1. Установите Streisand из Mac App Store.\n2. Нажмите «%s» ниже или используйте deep link:\n`streisand://import/%s`\n3. Выберите сервер и включите подключение.", t.BtnConnectOneClick, encodedSubURL)
		} else {
			guideText = fmt.Sprintf("Setup Guide for macOS:\n\n1. Install Streisand from Mac App Store.\n2. Tap '%s' below or open deep link:\n`streisand://import/%s`\n3. Select node and switch Connection to Connected.", t.BtnConnectOneClick, encodedSubURL)
		}

	case "linux":
		appDownloadURL = "https://github.com/SagerNet/sing-box/releases"
		appButtonText = "GitHub (Sing-box)"
		connectURL = fmt.Sprintf("%s/client/%s/connect?client=singbox", baseURL, token)
		if e.lang == i18n.RU {
			guideText = fmt.Sprintf("Инструкция по настройке для Linux:\n\n1. Скачайте sing-box или Hiddify с GitHub Releases.\n2. Нажмите «%s» ниже или используйте deep link:\n`sing-box://import-remote-profile?url=%s#Simple-VPN`\n3. Запустите sing-box daemon или приложение Hiddify.", t.BtnConnectOneClick, encodedSubURL)
		} else {
			guideText = fmt.Sprintf("Setup Guide for Linux:\n\n1. Download sing-box or Hiddify from GitHub Releases.\n2. Tap '%s' below or open deep link:\n`sing-box://import-remote-profile?url=%s#Simple-VPN`\n3. Run sing-box daemon or Hiddify GUI application.", t.BtnConnectOneClick, encodedSubURL)
		}

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

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(t.BtnConnectOneClick, connectURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(appButtonText, appDownloadURL),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("<< "+t.BtnDeviceWizard, "action:devices"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
		),
	)

	e.sendMessage(chatID, guideText, &keyboard)

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

func (e *BotEngine) sendQRCode(chatID int64, token string) {
	baseURL := e.cpClient.BaseURL()
	if baseURL == "" {
		baseURL = "http://localhost:8110"
	}
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
	_, _ = e.bot.Send(msg)
}

func (e *BotEngine) handlePreCheckoutQuery(query *tgbotapi.PreCheckoutQuery) {
	// Always approve Telegram Stars pre-checkout queries
	answer := tgbotapi.PreCheckoutConfig{
		OK:                 true,
		PreCheckoutQueryID: query.ID,
	}
	_, _ = e.bot.Request(answer)
}

func (e *BotEngine) handleSuccessfulPayment(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	orderIDStr := msg.SuccessfulPayment.InvoicePayload

	successMsg := fmt.Sprintf("Payment confirmed for order %s! Your subscription has been renewed.", orderIDStr[:8])
	if customReplies, err := e.cpClient.GetBotReplies(ctx); err == nil && customReplies != nil {
		if val, ok := customReplies["payment_success"]; ok && val != "" {
			if strings.Contains(val, "%s") {
				successMsg = fmt.Sprintf(val, orderIDStr[:8])
			} else {
				successMsg = val
			}
		}
	}

	e.sendMessage(chatID, successMsg, nil)
	e.handleStatus(ctx, chatID)
}

func (e *BotEngine) sendMessage(chatID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	_, _ = e.bot.Send(msg)
}

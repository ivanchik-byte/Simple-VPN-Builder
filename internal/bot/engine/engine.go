package engine

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/payment"
	"log/slog"
	"strings"
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

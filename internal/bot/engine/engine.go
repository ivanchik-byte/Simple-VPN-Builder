package engine

import (
	"context"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/payment"
	"log/slog"
	"strings"
	"time"
)

// dialogTTL bounds how long an awaiting_* conversation step stays valid.
const dialogTTL = 15 * time.Minute

type BotEngine struct {
	bot              *tgbotapi.BotAPI
	cpClient         *client.CPClient
	paymentMgr       *payment.Manager
	lang             i18n.Language
	userStates       map[int64]string    // chat_id -> state (e.g. awaiting_promo, awaiting_email_otp)
	userStateAt      map[int64]time.Time // chat_id -> when the state was set (dialog TTL)
	userPendingEmail map[int64]string    // chat_id -> pending email for OTP verification
	userPendingPromo map[int64]string    // chat_id -> validated promo code applied to next invoice
}

func NewBotEngine(bot *tgbotapi.BotAPI, cpClient *client.CPClient, paymentMgr *payment.Manager) *BotEngine {
	return &BotEngine{
		bot:              bot,
		cpClient:         cpClient,
		paymentMgr:       paymentMgr,
		lang:             i18n.EN,
		userStates:       make(map[int64]string),
		userStateAt:      make(map[int64]time.Time),
		userPendingEmail: make(map[int64]string),
		userPendingPromo: make(map[int64]string),
	}
}

// setUserState records a dialog step together with its timestamp for TTL sweeping.
func (e *BotEngine) setUserState(chatID int64, state string) {
	e.userStates[chatID] = state
	e.userStateAt[chatID] = time.Now()
}

// clearUserState drops the dialog step and any pending payloads for the chat.
func (e *BotEngine) clearUserState(chatID int64) {
	delete(e.userStates, chatID)
	delete(e.userStateAt, chatID)
	delete(e.userPendingEmail, chatID)
	delete(e.userPendingPromo, chatID)
}

// sweepExpiredStates removes dialog steps older than dialogTTL.
func (e *BotEngine) sweepExpiredStates(now time.Time) {
	for chatID, at := range e.userStateAt {
		if now.Sub(at) > dialogTTL {
			delete(e.userStates, chatID)
			delete(e.userStateAt, chatID)
			delete(e.userPendingEmail, chatID)
			delete(e.userPendingPromo, chatID)
		}
	}
}

// setLangFromCode switches the bundle by the Telegram client language.
// NOTE: the long-poll loop in Start is single-threaded — handleUpdate runs
// synchronously per update, so direct assignment to e.lang is race-free here.
func (e *BotEngine) setLangFromCode(code string) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(code)), "ru") {
		e.lang = i18n.RU
		return
	}
	e.lang = i18n.EN
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

	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		u := tgbotapi.NewUpdate(0)
		u.Timeout = 30

		updates := e.bot.GetUpdatesChan(u)

		// Inner long-poll loop: a panic in one update must not kill the bot,
		// and a closed updates channel triggers reconnect with backoff.
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Recovered from panic in updates loop", "panic", r)
				}
			}()
			for {
				select {
				case <-ctx.Done():
					return
				case update, ok := <-updates:
					if !ok {
						return
					}
					e.handleUpdateSafe(ctx, update)
				}
			}
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		slog.Warn("Telegram updates channel closed, reconnecting", "backoff", backoff.String())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// handleUpdateSafe wraps handleUpdate with recover so a single bad update
// cannot take down the whole long-poll loop.
func (e *BotEngine) handleUpdateSafe(ctx context.Context, update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Recovered from panic while handling telegram update", "panic", r)
		}
	}()
	e.handleUpdate(ctx, update)
}

func (e *BotEngine) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	e.sweepExpiredStates(time.Now())

	// Per-update language: Telegram clients report the UI language.
	switch {
	case update.Message != nil && update.Message.From != nil:
		e.setLangFromCode(update.Message.From.LanguageCode)
	case update.CallbackQuery != nil && update.CallbackQuery.From != nil:
		e.setLangFromCode(update.CallbackQuery.From.LanguageCode)
	case update.PreCheckoutQuery != nil:
		e.setLangFromCode(update.PreCheckoutQuery.From.LanguageCode)
	}

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
			customReplies, _ := e.cpClient.GetBotReplies(ctx)
			e.sendMessage(chatID, resolveBannedMessage(customReplies, t, reason), nil)
			return
		}
	}

	// Check if awaiting user state
	if state, ok := e.userStates[chatID]; ok {
		switch state {
		case "awaiting_promo":
			delete(e.userStates, chatID)
			delete(e.userStateAt, chatID)
			e.processPromoCode(ctx, chatID, text)
			return
		case "awaiting_email":
			delete(e.userStates, chatID)
			delete(e.userStateAt, chatID)
			e.processLinkEmail(ctx, chatID, text)
			return
		case "awaiting_email_otp":
			e.processVerifyEmailOTP(ctx, chatID, text, msg.From)
			return
		case "awaiting_restore":
			delete(e.userStates, chatID)
			delete(e.userStateAt, chatID)
			e.processRestoreAccount(ctx, chatID, msg.From, text)
			return
		case "awaiting_restore_otp":
			e.processVerifyRestoreOTP(ctx, chatID, text, msg.From)
			return
		}
	}

	if strings.HasPrefix(text, "/start") {
		// Fresh entry point: drop any stale dialog step.
		e.clearUserState(chatID)
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
		e.setUserState(chatID, "awaiting_email")
		e.sendMessage(chatID, t.EmailPrompt, nil)
		return
	}

	if strings.HasPrefix(text, "/restore") {
		parts := strings.Fields(text)
		if len(parts) > 1 {
			e.processRestoreAccount(ctx, chatID, msg.From, parts[1])
			return
		}
		e.setUserState(chatID, "awaiting_restore")
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
		e.setUserState(chatID, "awaiting_promo")
		e.sendMessage(chatID, t.PromoPrompt, nil)
	case t.BtnLinkEmail, "Привязать Email", "Link Recovery Email":
		e.setUserState(chatID, "awaiting_email")
		e.sendMessage(chatID, t.EmailPrompt, nil)
	case t.BtnRestore, "Восстановить по Email", "Restore Account":
		e.setUserState(chatID, "awaiting_restore")
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

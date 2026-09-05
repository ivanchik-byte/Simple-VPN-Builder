package engine

import (
	"context"
	"fmt"
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
	case "/reset", t.BtnResetKeys:
		e.handleResetKeys(ctx, chatID)
	case "/servers", t.BtnNodes:
		e.handleServerList(ctx, chatID)
	case "/promo", t.BtnEnterPromo:
		e.userStates[chatID] = "awaiting_promo"
		e.sendMessage(chatID, t.PromoPrompt, nil)
	case "/help", t.BtnHelp:
		e.sendMessage(chatID, t.HelpText, nil)
	default:
		e.handleStart(ctx, msg, "")
	}
}

func (e *BotEngine) handleStart(ctx context.Context, msg *tgbotapi.Message, refCode string) {
	t := i18n.GetBundle(e.lang)
	chatID := msg.Chat.ID

	// Query Control Plane for user
	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil {
		e.sendMessage(chatID, "Error communicating with control plane. Please try again later.", nil)
		return
	}

	if userRes != nil {
		// Existing subscriber
		if userRes.User.IsBanned.Bool {
			e.sendMessage(chatID, fmt.Sprintf(t.BannedMessage, userRes.User.BanReason.String), nil)
			return
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
				tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.BtnResetKeys, "action:reset_keys"),
				tgbotapi.NewInlineKeyboardButtonData(t.BtnNodes, "action:nodes"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(t.BtnEnterPromo, "action:promo"),
				tgbotapi.NewInlineKeyboardButtonData(t.BtnHelp, "action:help"),
			),
		)

		text := fmt.Sprintf(t.WelcomeActiveUser,
			i18n.FormatBytes(userRes.User.TrafficUsed.Int64),
			i18n.FormatBytes(userRes.User.TrafficLimit.Int64),
			userRes.User.ExpiresAt.Time.Format("2006-01-02 15:04"),
			userRes.SubscriptionURL,
		)
		e.sendMessage(chatID, text, &keyboard)
		return
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
		e.sendMessage(chatID, t.WelcomeNewUser, &keyboard)
		return
	}

	// No trial: direct to purchase
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnHelp, "action:help"),
		),
	)
	e.sendMessage(chatID, t.WelcomeNewUser+"\n\n"+t.NoTrialAvailable, &keyboard)
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
		e.handleResetKeys(ctx, chatID)
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

	if data == "action:help" {
		t := i18n.GetBundle(e.lang)
		e.sendMessage(chatID, t.HelpText, nil)
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

	text := fmt.Sprintf(t.TrialActivated,
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

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Get QR Code", fmt.Sprintf("action:qr:%s", userRes.SubscriptionToken)),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnResetKeys, "action:reset_keys"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnRenew, "action:buy"),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnNodes, "action:nodes"),
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
	e.sendMessage(chatID, t.SelectPlan, &keyboard)
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

func (e *BotEngine) handleSelectPayment(_ context.Context, chatID int64, planID string, months int32) {
	t := i18n.GetBundle(e.lang)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Telegram Stars (In-App)", fmt.Sprintf("pay:%s:%d:stars", planID, months)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("CryptoBot (USDT, TON, BTC)", fmt.Sprintf("pay:%s:%d:cryptobot", planID, months)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(t.BtnBack, fmt.Sprintf("plan:%s", planID)),
		),
	)

	e.sendMessage(chatID, t.SelectPayment, &keyboard)
}

func (e *BotEngine) handleCheckout(ctx context.Context, chatID int64, planIDStr string, months int32, gateway string) {
	t := i18n.GetBundle(e.lang)

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

func (e *BotEngine) handleResetKeys(ctx context.Context, chatID int64) {
	t := i18n.GetBundle(e.lang)

	userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID)
	if err != nil || userRes == nil {
		e.sendMessage(chatID, "No subscription found to rotate. Tap /start to begin.", nil)
		return
	}

	rotated, err := e.cpClient.RotateKeys(ctx, userRes.User.ID)
	if err != nil {
		e.sendMessage(chatID, fmt.Sprintf("Failed to rotate keys: %v", err), nil)
		return
	}

	text := fmt.Sprintf(t.KeysResetSuccess, rotated.SubscriptionURL)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Get QR Code", fmt.Sprintf("action:qr:%s", rotated.SubscriptionToken)),
			tgbotapi.NewInlineKeyboardButtonData(t.BtnStatus, "action:status"),
		),
	)

	e.sendMessage(chatID, text, &keyboard)
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
	subURL := fmt.Sprintf("/sub/%s", token)
	pngBytes, err := qrcode.Encode(subURL, qrcode.Medium, 256)
	if err != nil {
		e.sendMessage(chatID, "Failed to render QR code.", nil)
		return
	}

	photoFile := tgbotapi.FileBytes{
		Name:  "subscription_qr.png",
		Bytes: pngBytes,
	}

	msg := tgbotapi.NewPhoto(chatID, photoFile)
	msg.Caption = fmt.Sprintf("Universal Subscription QR Code\nToken: %s", token[:8])
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

	// Post webhook directly or via webhook route
	e.sendMessage(chatID, fmt.Sprintf("Payment confirmed for order %s! Your subscription has been renewed.", orderIDStr[:8]), nil)
	e.handleStatus(ctx, chatID)
}

func (e *BotEngine) sendMessage(chatID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}
	_, _ = e.bot.Send(msg)
}

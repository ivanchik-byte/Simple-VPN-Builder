package engine

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/payment"
	"net/url"
	"strconv"
	"strings"
)

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

	// Mandatory Telegram channel membership check if configured
	if replies != nil && replies["require_channel_sub"] == "true" {
		channelLink := strings.TrimSpace(replies["channel_link"])
		if channelLink != "" && e.bot != nil {
			channelUsername := strings.TrimPrefix(channelLink, "https://t.me/")
			channelUsername = strings.TrimPrefix(channelUsername, "t.me/")
			channelUsername = strings.TrimPrefix(channelUsername, "@")
			if !strings.Contains(channelUsername, "/") && channelUsername != "" {
				chatMember, err := e.bot.GetChatMember(tgbotapi.GetChatMemberConfig{
					ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
						SuperGroupUsername: "@" + channelUsername,
						UserID:             chatID,
					},
				})
				if err == nil && (chatMember.Status == "left" || chatMember.Status == "kicked") {
					subKeyboard := tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonURL("📢 Join Official Channel", channelLink),
						),
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("✅ Check & Continue", "action:trial"),
						),
					)
					msgText := "To activate your complimentary trial access, please subscribe to our official community channel first:"
					if e.lang == i18n.RU {
						msgText = "Для активации бесплатного пробного периода, пожалуйста, подпишитесь на наш официальный канал:"
					}
					e.sendMessage(chatID, msgText, &subKeyboard)
					return
				}
			}
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

	var text string
	if strings.Contains(trialTpl, "%") {
		// Positional format (%s, %d)
		text = fmt.Sprintf(trialTpl,
			i18n.FormatBytes(trialRes.TrafficLimitBytes),
			trialRes.TrialHours,
			trialRes.SubscriptionURL,
		)
	} else {
		// Named tags template ({traffic_total}, {days_left}, {sub_url})
		text = trialTpl
	}

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

func (e *BotEngine) processPromoCode(ctx context.Context, chatID int64, code string) {
	t := i18n.GetBundle(e.lang)

	promo, err := e.cpClient.ValidatePromo(ctx, code)
	if err != nil || promo == nil {
		e.sendMessage(chatID, t.PromoInvalid, nil)
		return
	}

	e.sendMessage(chatID, fmt.Sprintf(t.PromoSuccess, promo.Code), nil)
}

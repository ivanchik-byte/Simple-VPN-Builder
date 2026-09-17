package engine

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	qrcode "github.com/skip2/go-qrcode"
	"html"
	"log/slog"
	"net/url"
	"strings"
)

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
		// Node names are operator input rendered in HTML mode: escape them.
		sb.WriteString(fmt.Sprintf("- %s [%s] - %s (Capacity: %d Gbps)\n", html.EscapeString(n.Name), html.EscapeString(region), html.EscapeString(status), n.CapacityGbps.Int32))
	}

	text := fmt.Sprintf(t.NodeListHeader, sb.String())
	e.sendMessage(chatID, text, nil)
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
		e.setUserState(chatID, "awaiting_email_otp")
		e.userPendingEmail[chatID] = email

		prompt := fmt.Sprintf(t.OTPPrompt, html.EscapeString(email))
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

	e.sendMessage(chatID, fmt.Sprintf(t.EmailLinkedSuccess, html.EscapeString(email)), nil)
}

func (e *BotEngine) processVerifyEmailOTP(ctx context.Context, chatID int64, otp string, from *tgbotapi.User) {
	t := i18n.GetBundle(e.lang)
	otp = strings.TrimSpace(otp)
	email := e.userPendingEmail[chatID]
	if email == "" {
		delete(e.userStates, chatID)
		delete(e.userStateAt, chatID)
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
			delete(e.userStateAt, chatID)
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
	delete(e.userStateAt, chatID)
	delete(e.userPendingEmail, chatID)
	e.sendMessage(chatID, fmt.Sprintf(t.EmailLinkedSuccess, html.EscapeString(email)), nil)
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
		e.setUserState(chatID, "awaiting_restore_otp")
		e.userPendingEmail[chatID] = email

		prompt := fmt.Sprintf(t.OTPPrompt, html.EscapeString(email))
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
		delete(e.userStateAt, chatID)
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
			delete(e.userStateAt, chatID)
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
	delete(e.userStateAt, chatID)
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

func (e *BotEngine) sendQRCode(ctx context.Context, chatID int64, token string) {
	if len(token) < 8 {
		e.sendMessage(chatID, "QR code unavailable: invalid subscription reference.", nil)
		return
	}
	// Defense in depth: re-verify ownership even when the caller checked.
	if userRes, err := e.cpClient.GetUserByTelegramID(ctx, chatID); err != nil || userRes == nil {
		e.sendMessage(chatID, "QR code unavailable: subscription not found.", nil)
		return
	} else if userRes.SubscriptionToken != token {
		e.sendMessage(chatID, "QR code unavailable for this subscription.", nil)
		return
	}
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
	msg.Caption = fmt.Sprintf("Universal Subscription QR Code\nToken: %s\nURL: %s", shortToken(token), fullSubURL)
	if _, err := e.bot.Send(msg); err != nil {
		slog.Error("Failed to send QR code photo", "chat_id", chatID, "error", err)
	}
}

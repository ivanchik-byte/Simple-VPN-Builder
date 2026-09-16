package engine

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

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
		var photo tgbotapi.PhotoConfig
		if strings.HasPrefix(mediaURL, "http://") || strings.HasPrefix(mediaURL, "https://") {
			photo = tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(mediaURL))
		} else {
			cleanPath := strings.TrimPrefix(mediaURL, "/")
			possiblePaths := []string{
				mediaURL,
				filepath.Join("data", cleanPath),
				filepath.Join(".", mediaURL),
				filepath.Join("/app/data", cleanPath),
			}
			found := false
			for _, p := range possiblePaths {
				if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
					photo = tgbotapi.NewPhoto(chatID, tgbotapi.FilePath(p))
					found = true
					break
				}
			}
			if !found {
				photo = tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(mediaURL))
			}
		}

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

package engine

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"os"
	"strconv"
	"strings"
	"time"
)

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

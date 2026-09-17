package engine

import (
	"context"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"html"
	"os"
	"strconv"
	"strings"
	"time"
)

func shortOrderID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// shortToken renders a safe token prefix; never slices without a length guard.
func shortToken(token string) string {
	if len(token) > 8 {
		return token[:8]
	}
	return token
}

// resolveBannedMessage renders the ban notice: admin-editable "banned_message"
// template wins, i18n bundle is the fallback. Supports both the legacy
// positional "%s" style and a "{ban_reason}" named tag.
func resolveBannedMessage(customReplies map[string]string, t i18n.TranslationBundle, reason string) string {
	tpl := t.BannedMessage
	if customReplies != nil {
		if val, ok := customReplies["banned_message"]; ok && strings.TrimSpace(val) != "" {
			tpl = val
		}
	}
	// reason is operator-entered; escape so it cannot inject Telegram HTML.
	escaped := html.EscapeString(reason)
	if strings.Contains(tpl, "%s") {
		return fmt.Sprintf(tpl, escaped)
	}
	return strings.ReplaceAll(tpl, "{ban_reason}", escaped)
}

// isTrialAlreadyClaimed reports whether a CreateTrial error means the Telegram
// account already consumed its trial (control plane answers 409
// "Free trial already claimed for this Telegram account").
func isTrialAlreadyClaimed(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "409") || strings.Contains(msg, "already claimed")
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
	shortID := shortOrderID(orderIDStr)

	if orderUUID, err := uuid.Parse(strings.TrimSpace(orderIDStr)); err == nil {
		if err := e.cpClient.ConfirmStarsPayment(ctx, orderUUID, chatID); err != nil {
			e.sendMessage(chatID, fmt.Sprintf("Payment received for order %s, but confirmation failed: %v. Support will reconcile it shortly.", shortID, err), nil)
			return
		}
	} else {
		e.sendMessage(chatID, "Payment received, but the order reference is invalid. Support will reconcile it shortly.", nil)
		return
	}

	successMsg := fmt.Sprintf("Payment confirmed for order %s! Your subscription has been renewed.", shortID)
	customReplies, _ := e.cpClient.GetBotReplies(ctx)
	if customReplies != nil {
		if val, ok := customReplies["payment_success"]; ok && val != "" {
			if strings.Contains(val, "%s") {
				successMsg = fmt.Sprintf(val, shortID)
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
		if userRes.User.TelegramFirstName.Valid && strings.TrimSpace(userRes.User.TelegramFirstName.String) != "" {
			firstName = userRes.User.TelegramFirstName.String
		} else if userRes.User.Username != "" && !strings.HasPrefix(userRes.User.Username, "tg_") {
			firstName = userRes.User.Username
		} else if userRes.User.TelegramUsername.Valid && strings.TrimSpace(userRes.User.TelegramUsername.String) != "" {
			firstName = strings.TrimPrefix(strings.TrimSpace(userRes.User.TelegramUsername.String), "@")
		}
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

	// Escape every substitution: admin templates may opt into Telegram HTML
	// (sendMessage switches to ModeHTML on any "<...>" in the final text), so a
	// first name like "<b>test</b>" must not break markup or inject tags.
	// Static template text itself is left untouched.
	esc := html.EscapeString
	r := strings.NewReplacer(
		"{username}", esc(username),
		"{first_name}", esc(firstName),
		"{user_id}", esc(userIDStr),
		"{balance}", "0.00",
		"{currency}", esc("USD"),
		"{refprocent}", esc(refPercent),
		"{ref_link}", esc(refLink),
		"{ref_count}", esc(strconv.FormatInt(refCount, 10)),
		"{ref_days}", esc(strconv.FormatInt(refDays, 10)),
		"{traffic_used}", esc(trafficUsed),
		"{traffic_total}", esc(trafficTotal),
		"{traffic_left}", esc(trafficLeft),
		"{expires_at}", esc(expiresAt),
		"{days_left}", esc(daysLeft),
		"{sub_url}", esc(subURL),
		"{portal_url}", esc(portalURL),
		"{plan_name}", esc(planName),
	)
	return r.Replace(text)
}

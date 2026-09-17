package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/i18n"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type BotSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
}

type ReminderWorker struct {
	bot            BotSender
	userRepo       store.UserRepository
	interval       time.Duration
	lang           i18n.Language
	mu             sync.Mutex
	lastQuotaWarn  map[int64]time.Time
	lastExpiryWarn map[int64]time.Time
	repliesMu      sync.RWMutex
	botReplies     map[string]string
}

func NewReminderWorker(bot BotSender, userRepo store.UserRepository, interval time.Duration) *ReminderWorker {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	return &ReminderWorker{
		bot:            bot,
		userRepo:       userRepo,
		interval:       interval,
		lang:           i18n.EN,
		lastQuotaWarn:  make(map[int64]time.Time),
		lastExpiryWarn: make(map[int64]time.Time),
	}
}

// SetBotReplies injects admin-customized bot templates (bot_replies table).
// Missing/empty keys fall back to the i18n bundle. Safe for concurrent use.
func (w *ReminderWorker) SetBotReplies(replies map[string]string) {
	w.repliesMu.Lock()
	defer w.repliesMu.Unlock()
	w.botReplies = replies
}

func (w *ReminderWorker) replyText(key, fallback string) string {
	w.repliesMu.RLock()
	defer w.repliesMu.RUnlock()
	if v, ok := w.botReplies[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func (w *ReminderWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runCheck(ctx)
		}
	}
}

func (w *ReminderWorker) runCheck(ctx context.Context) {
	t := i18n.GetBundle(w.lang)

	users, _, err := w.userRepo.List(ctx, store.UserFilter{
		Status: "active",
		Limit:  1000,
	})
	if err != nil {
		return
	}

	now := time.Now()

	for _, user := range users {
		if !user.TelegramID.Valid || user.TelegramID.Int64 <= 0 {
			continue
		}

		chatID := user.TelegramID.Int64

		w.mu.Lock()
		lastQuota, hadQuota := w.lastQuotaWarn[chatID]
		lastExpiry, hadExpiry := w.lastExpiryWarn[chatID]
		w.mu.Unlock()

		// 1. Quota check: 80% threshold with 24-hour cooldown
		if user.TrafficLimit.Valid && user.TrafficLimit.Int64 > 0 {
			usedPercent := float64(user.TrafficUsed.Int64) / float64(user.TrafficLimit.Int64)
			if usedPercent >= 0.80 && usedPercent < 0.95 {
				if !hadQuota || now.Sub(lastQuota) >= 24*time.Hour {
					usedStr := i18n.FormatBytes(user.TrafficUsed.Int64)
					totalStr := i18n.FormatBytes(user.TrafficLimit.Int64)
					msgText := renderReminderTemplate(
						w.replyText("quota_warning_80", t.QuotaWarning80),
						map[string]string{
							"{traffic_used}":  usedStr,
							"{traffic_total}": totalStr,
						},
						[]string{usedStr, totalStr},
					)
					msg := tgbotapi.NewMessage(chatID, msgText)
					if _, err := w.bot.Send(msg); err == nil {
						w.mu.Lock()
						w.lastQuotaWarn[chatID] = now
						w.mu.Unlock()
					}
				}
			}
		}

		// 2. Expiration check: within 24 hours with 24-hour cooldown
		if user.ExpiresAt.Valid {
			remaining := user.ExpiresAt.Time.Sub(now)
			if remaining > 0 && remaining <= 24*time.Hour {
				if !hadExpiry || now.Sub(lastExpiry) >= 24*time.Hour {
					expiryStr := user.ExpiresAt.Time.Format("2006-01-02 15:04")
					msgText := renderReminderTemplate(
						w.replyText("expiry_warning_24h", t.ExpiryWarning24h),
						map[string]string{
							"{expires_at}": expiryStr,
						},
						[]string{expiryStr},
					)
					msg := tgbotapi.NewMessage(chatID, msgText)
					if _, err := w.bot.Send(msg); err == nil {
						w.mu.Lock()
						w.lastExpiryWarn[chatID] = now
						w.mu.Unlock()
					}
				}
			}
		}
	}
}

// renderReminderTemplate fills a reminder template. Positional "%s" verbs are
// filled from positional args (keeps i18n defaults working); if the admin
// template uses named {tags} instead, those are substituted from named.
func renderReminderTemplate(tpl string, named map[string]string, positional []string) string {
	if strings.Contains(tpl, "%s") && len(positional) > 0 {
		args := make([]any, len(positional))
		for i, v := range positional {
			args[i] = v
		}
		return fmt.Sprintf(tpl, args...)
	}
	out := tpl
	for tag, val := range named {
		out = strings.ReplaceAll(out, tag, val)
	}
	return out
}

// Note: UI templates subscription_expired, expiry_warning_72h, trial_expiring_soon,
// and traffic_limit_reached will be wired once transition triggers are implemented.

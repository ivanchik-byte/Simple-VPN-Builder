package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type mockBotSender struct {
	mu       sync.Mutex
	messages []tgbotapi.Chattable
}

func (m *mockBotSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, c)
	return tgbotapi.Message{MessageID: len(m.messages)}, nil
}

func (m *mockBotSender) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

type mockReminderUserRepo struct {
	store.UserRepository
	users []store.User
}

func (m *mockReminderUserRepo) List(_ context.Context, _ store.UserFilter) ([]store.User, int64, error) {
	return m.users, int64(len(m.users)), nil
}

func TestReminderWorker_AntiSpam_Cooldown(t *testing.T) {
	bot := &mockBotSender{}
	userRepo := &mockReminderUserRepo{}

	userID := uuid.New()
	var chatID int64 = 987654321

	// User has 85% traffic used and expires in 12 hours
	userRepo.users = []store.User{
		{
			ID:           userID,
			TelegramID:   pgtype.Int8{Int64: chatID, Valid: true},
			TrafficLimit: pgtype.Int8{Int64: 100 * 1024 * 1024 * 1024, Valid: true},
			TrafficUsed:  pgtype.Int8{Int64: 85 * 1024 * 1024 * 1024, Valid: true},
			ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(12 * time.Hour), Valid: true},
			Status:       pgtype.Text{String: "active", Valid: true},
		},
	}

	w := NewReminderWorker(bot, userRepo, 1*time.Hour)
	ctx := context.Background()

	// 1. First run: both quota and expiry warning must be sent (2 messages)
	w.runCheck(ctx)
	require.Equal(t, 2, bot.Count(), "expected initial quota and expiry warnings")

	// 2. Second run immediate: should NOT send duplicate messages because 24h cooldown is active
	w.runCheck(ctx)
	assert.Equal(t, 2, bot.Count(), "cooldown must prevent duplicate spam")

	// 3. Third run simulating 25 hours later: cooldown expired, warning can be sent again
	w.mu.Lock()
	pastTime := time.Now().Add(-25 * time.Hour)
	w.lastQuotaWarn[chatID] = pastTime
	w.lastExpiryWarn[chatID] = pastTime
	w.mu.Unlock()

	w.runCheck(ctx)
	assert.Equal(t, 4, bot.Count(), "after 24h cooldown, new warnings can be sent")
}

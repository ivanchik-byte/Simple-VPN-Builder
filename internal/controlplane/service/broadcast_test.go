package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBroadcastBillingRepo struct {
	store.BillingRepository
	mu        sync.Mutex
	campaigns map[uuid.UUID]store.BroadcastCampaign
}

func newMockBroadcastBillingRepo() *mockBroadcastBillingRepo {
	return &mockBroadcastBillingRepo{
		campaigns: make(map[uuid.UUID]store.BroadcastCampaign),
	}
}

func (m *mockBroadcastBillingRepo) CreateBroadcastCampaign(_ context.Context, params store.CreateBroadcastCampaignParams) (store.BroadcastCampaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New()
	c := store.BroadcastCampaign{
		ID:              id,
		Title:           params.Title,
		TargetSegment:   params.TargetSegment,
		MessageText:     params.MessageText,
		InlineButtons:   params.InlineButtons,
		TotalRecipients: params.TotalRecipients,
		Status:          params.Status,
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.campaigns[id] = c
	return c, nil
}

func (m *mockBroadcastBillingRepo) GetBroadcastCampaign(_ context.Context, id uuid.UUID) (store.BroadcastCampaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.campaigns[id]
	if !ok {
		return store.BroadcastCampaign{}, errors.New("campaign not found")
	}
	return c, nil
}

func (m *mockBroadcastBillingRepo) UpdateBroadcastCampaignStats(_ context.Context, params store.UpdateBroadcastCampaignStatsParams) (store.BroadcastCampaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.campaigns[params.ID]
	if !ok {
		return store.BroadcastCampaign{}, errors.New("campaign not found")
	}
	c.SentCount = params.SentCount
	c.FailedCount = params.FailedCount
	c.Status = params.Status
	c.CompletedAt = params.CompletedAt
	m.campaigns[params.ID] = c
	return c, nil
}

func (m *mockBroadcastBillingRepo) ListBroadcastCampaigns(_ context.Context) ([]store.BroadcastCampaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var list []store.BroadcastCampaign
	for _, c := range m.campaigns {
		list = append(list, c)
	}
	return list, nil
}

type mockBroadcastUserRepo struct {
	store.UserRepository
	recipients map[string][]int64
}

func (m *mockBroadcastUserRepo) ListTelegramIDsForBroadcast(_ context.Context, segment string) ([]int64, error) {
	return m.recipients[segment], nil
}

type mockTelegramSender struct {
	mu       sync.Mutex
	messages []struct {
		chatID int64
		text   string
	}
	shouldFail bool
}

func (m *mockTelegramSender) SendMessage(_ context.Context, chatID int64, text string, _ []BroadcastButton) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return errors.New("blocked by user")
	}
	m.messages = append(m.messages, struct {
		chatID int64
		text   string
	}{chatID: chatID, text: text})
	return nil
}

func TestBroadcastService_CreateAndDispatch_Success(t *testing.T) {
	billingRepo := newMockBroadcastBillingRepo()
	userRepo := &mockBroadcastUserRepo{
		recipients: map[string][]int64{
			"all": {1001, 1002, 1003},
		},
	}
	sender := &mockTelegramSender{}

	svc := NewBroadcastService(billingRepo, userRepo, sender, nil)
	svc.SetRateDelay(1 * time.Millisecond) // Fast for testing

	buttons := []BroadcastButton{
		{Text: "Renew Now", URL: "https://t.me/bot?start=buy"},
	}

	campaign, err := svc.CreateAndDispatch(context.Background(), "Summer Sale", "all", "50% off all plans", buttons)
	require.NoError(t, err)
	assert.Equal(t, "Summer Sale", campaign.Title)
	assert.Equal(t, int32(3), campaign.TotalRecipients.Int32)

	// Wait for async dispatch to complete
	require.Eventually(t, func() bool {
		c, err := svc.GetCampaign(context.Background(), campaign.ID)
		return err == nil && c.Status.String == "completed"
	}, 2*time.Second, 10*time.Millisecond)

	final, err := svc.GetCampaign(context.Background(), campaign.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(3), final.SentCount.Int32)
	assert.Equal(t, int32(0), final.FailedCount.Int32)

	sender.mu.Lock()
	assert.Len(t, sender.messages, 3)
	sender.mu.Unlock()
}

func TestBroadcastService_CreateAndDispatch_Failure(t *testing.T) {
	billingRepo := newMockBroadcastBillingRepo()
	userRepo := &mockBroadcastUserRepo{
		recipients: map[string][]int64{
			"expired": {2001, 2002},
		},
	}
	sender := &mockTelegramSender{shouldFail: true}

	svc := NewBroadcastService(billingRepo, userRepo, sender, nil)
	svc.SetRateDelay(1 * time.Millisecond)

	campaign, err := svc.CreateAndDispatch(context.Background(), "We Miss You", "expired", "Come back with 20% discount", nil)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		c, err := svc.GetCampaign(context.Background(), campaign.ID)
		return err == nil && c.Status.String == "failed"
	}, 2*time.Second, 10*time.Millisecond)

	final, err := svc.GetCampaign(context.Background(), campaign.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(0), final.SentCount.Int32)
	assert.Equal(t, int32(2), final.FailedCount.Int32)
}

func TestHTTPTelegramSender_SendMessage(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bottest-token-123/sendMessage", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer server.Close()

	sender := NewHTTPTelegramSender("test-token-123", server.URL)
	buttons := []BroadcastButton{
		{Text: "Test Link", URL: "https://example.com"},
	}

	err := sender.SendMessage(context.Background(), 123456789, "Hello World", buttons)
	require.NoError(t, err)

	assert.Equal(t, float64(123456789), receivedBody["chat_id"])
	assert.Equal(t, "Hello World", receivedBody["text"])
	assert.NotNil(t, receivedBody["reply_markup"])
}

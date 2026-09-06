package alerting

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertDispatcher_SendTelegramWithThreadID(t *testing.T) {
	var receivedRequests []map[string]any
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bottest-token/sendMessage", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var payload map[string]any
		err = json.Unmarshal(body, &payload)
		require.NoError(t, err)

		mu.Lock()
		receivedRequests = append(receivedRequests, payload)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := AlertConfig{
		BotToken:     "test-token",
		ChatID:       -100123456789,
		TopicInfra:   101,
		TopicAudit:   202,
		TopicBilling: 303,
		Enabled:      true,
	}

	dispatcher := NewAlertDispatcher(cfg)
	dispatcher.SetBaseURL(server.URL)
	defer dispatcher.Stop()

	// 1. Send Infra Alert
	dispatcher.SendInfraAlert("nl-ams-1", "offline", "Node heartbeat timed out after 30s")

	// 2. Send Audit Alert
	dispatcher.SendAuditAlert("support_admin", "ResetUserTraffic", "user_bob", "Traffic reset to 0 bytes")

	// 3. Send Billing Alert
	dispatcher.SendBillingAlert("CryptoBot", "15.00 USDT", "user_alice", "INV-999")

	// Wait for background worker
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, receivedRequests, 3)

	// Check Infra
	req1 := receivedRequests[0]
	assert.Equal(t, float64(-100123456789), req1["chat_id"])
	assert.Equal(t, float64(101), req1["message_thread_id"])
	assert.Contains(t, req1["text"], "[ALERT] Node: nl-ams-1")
	assert.Contains(t, req1["text"], "Status: offline")

	// Check Audit
	req2 := receivedRequests[1]
	assert.Equal(t, float64(202), req2["message_thread_id"])
	assert.Contains(t, req2["text"], "[AUDIT] Action: ResetUserTraffic")
	assert.Contains(t, req2["text"], "Actor: support_admin")

	// Check Billing
	req3 := receivedRequests[2]
	assert.Equal(t, float64(303), req3["message_thread_id"])
	assert.Contains(t, req3["text"], "[BILLING] Payment Received")
	assert.Contains(t, req3["text"], "Gateway: CryptoBot")
}

func TestAlertDispatcher_DisabledDoesNotSend(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := AlertConfig{
		BotToken: "test-token",
		ChatID:   -100123456789,
		Enabled:  false,
	}

	dispatcher := NewAlertDispatcher(cfg)
	dispatcher.SetBaseURL(server.URL)
	defer dispatcher.Stop()

	dispatcher.SendInfraAlert("de-fra-1", "online", "Recovered")
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 0, callCount)
}

func TestAlertDispatcher_SendRBACAlert(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/bottest_token/sendMessage", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	d := NewAlertDispatcher(AlertConfig{
		BotToken:   "test_token",
		ChatID:     123456,
		TopicAudit: 99,
		Enabled:    true,
	})
	defer d.Stop()
	d.SetBaseURL(server.URL)

	changes := []RBACDiffItem{
		{
			Field:    "can_broadcast",
			OldValue: false,
			NewValue: true,
			Granted:  true,
		},
		{
			Field:    "can_manage_nodes",
			OldValue: true,
			NewValue: false,
			Granted:  false,
		},
	}

	d.SendRBACAlert("owner@vpn.local", "owner", "operator@vpn.local", "admin", "192.168.1.50", "SUCCESS", changes, "Permissions updated")

	time.Sleep(200 * time.Millisecond)

	assert.NotNil(t, receivedBody)
	assert.Equal(t, float64(123456), receivedBody["chat_id"])
	assert.Equal(t, float64(99), receivedBody["message_thread_id"])
	assert.Equal(t, "HTML", receivedBody["parse_mode"])

	text, ok := receivedBody["text"].(string)
	assert.True(t, ok)
	assert.Contains(t, text, "[RBAC PERMISSION CHANGED]")
	assert.Contains(t, text, "owner@vpn.local")
	assert.Contains(t, text, "operator@vpn.local")
	assert.Contains(t, text, "+ [GRANTED] can_broadcast")
	assert.Contains(t, text, "- [REVOKED] can_manage_nodes")
}

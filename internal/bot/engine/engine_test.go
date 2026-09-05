package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedMessage struct {
	ChatID      int64
	Text        string
	ReplyMarkup *tgbotapi.InlineKeyboardMarkup
}

type recordedDocument struct {
	ChatID   int64
	FileName string
	Caption  string
	Content  []byte
}

type mockBotTransport struct {
	mu        sync.Mutex
	messages  []recordedMessage
	documents []recordedDocument
}

func setupTestBotAndCP(t *testing.T, user *store.User, subToken string) (*BotEngine, *mockBotTransport, *httptest.Server, *httptest.Server) {
	trans := &mockBotTransport{}

	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/botmock-token/getMe" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":         1,
					"is_bot":     true,
					"first_name": "VPNBot",
					"username":   "vpn_bot",
				},
			})
			return
		}

		if r.URL.Path == "/botmock-token/sendMessage" {
			_ = r.ParseForm()
			text := r.FormValue("text")
			chatIDStr := r.FormValue("chat_id")
			chatID, _ := strconv.ParseInt(chatIDStr, 10, 64)
			markupStr := r.FormValue("reply_markup")
			var markup *tgbotapi.InlineKeyboardMarkup
			if markupStr != "" {
				var km tgbotapi.InlineKeyboardMarkup
				if err := json.Unmarshal([]byte(markupStr), &km); err == nil {
					markup = &km
				}
			}
			trans.mu.Lock()
			trans.messages = append(trans.messages, recordedMessage{
				ChatID:      chatID,
				Text:        text,
				ReplyMarkup: markup,
			})
			trans.mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 100},
			})
			return
		}

		if r.URL.Path == "/botmock-token/sendDocument" {
			bodyBytes, _ := io.ReadAll(r.Body)
			trans.mu.Lock()
			trans.documents = append(trans.documents, recordedDocument{
				ChatID:   12345,
				FileName: "config.conf",
				Caption:  "VPN Configuration",
				Content:  bodyBytes,
			})
			trans.mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]any{"message_id": 101},
			})
			return
		}

		if r.URL.Path == "/botmock-token/answerCallbackQuery" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": true,
			})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	botAPI, err := tgbotapi.NewBotAPIWithClient("mock-token", tgServer.URL+"/bot%s/%s", tgServer.Client())
	require.NoError(t, err)

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/v1/users/by-telegram/12345" {
			if user == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(client.UserWithSubscription{
				User:              *user,
				SubscriptionToken: subToken,
				SubscriptionURL:   "/sub/" + subToken,
			})
			return
		}

		if strings.HasSuffix(r.URL.Path, "/referrals") {
			_ = json.NewEncoder(w).Encode(client.ReferralStatsResponse{
				TelegramID:           12345,
				ReferralCode:         "ref_12345",
				ReferralCount:        3,
				BonusDaysPerReferral: 7,
			})
			return
		}

		if r.URL.Path == "/api/v1/users/"+user.ID.String()+"/rotate" {
			newSubToken := uuid.New().String()
			_ = json.NewEncoder(w).Encode(client.RotateResponse{
				SubscriptionToken: newSubToken,
				SubscriptionURL:   "/sub/" + newSubToken,
			})
			return
		}

		if r.URL.Path == "/sub/"+subToken || len(r.URL.Path) > 5 {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("[Interface]\nPrivateKey = mock-privkey\nAddress = 10.0.0.2/32\n"))
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	cpClient := client.NewCPClient(cpServer.URL, "test-api-key")
	botEngine := NewBotEngine(botAPI, cpClient, nil)

	return botEngine, trans, tgServer, cpServer
}

func TestBotEngine_ResetPrompt(t *testing.T) {
	userID := uuid.New()
	subToken := uuid.New().String()
	user := &store.User{
		ID:           userID,
		Username:     "testuser",
		Status:       pgtype.Text{String: "active", Valid: true},
		TrafficUsed:  pgtype.Int8{Int64: 1024, Valid: true},
		TrafficLimit: pgtype.Int8{Int64: 1048576, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}

	engine, trans, tgSrv, cpSrv := setupTestBotAndCP(t, user, subToken)
	defer tgSrv.Close()
	defer cpSrv.Close()

	ctx := context.Background()

	// Trigger /reset command via message
	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 12345},
		Text: "/reset",
	}
	engine.handleMessage(ctx, msg)

	trans.mu.Lock()
	require.NotEmpty(t, trans.messages)
	lastMsg := trans.messages[len(trans.messages)-1]
	trans.mu.Unlock()

	assert.Contains(t, lastMsg.Text, "Are you sure")
	require.NotNil(t, lastMsg.ReplyMarkup)
	require.NotEmpty(t, lastMsg.ReplyMarkup.InlineKeyboard)

	// Verify buttons for confirm and cancel
	firstRow := lastMsg.ReplyMarkup.InlineKeyboard[0]
	assert.Equal(t, "action:confirm_reset_keys", *firstRow[0].CallbackData)
	assert.Equal(t, "action:cancel_reset", *firstRow[1].CallbackData)
}

func TestBotEngine_ConfirmResetKeys(t *testing.T) {
	userID := uuid.New()
	subToken := uuid.New().String()
	user := &store.User{
		ID:           userID,
		Username:     "testuser",
		Status:       pgtype.Text{String: "active", Valid: true},
		TrafficUsed:  pgtype.Int8{Int64: 1024, Valid: true},
		TrafficLimit: pgtype.Int8{Int64: 1048576, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}

	engine, trans, tgSrv, cpSrv := setupTestBotAndCP(t, user, subToken)
	defer tgSrv.Close()
	defer cpSrv.Close()

	ctx := context.Background()

	// Send callback query action:confirm_reset_keys
	cb := &tgbotapi.CallbackQuery{
		ID:   "cb123",
		Data: "action:confirm_reset_keys",
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 12345},
		},
	}
	engine.handleCallbackQuery(ctx, cb)

	trans.mu.Lock()
	docsCount := len(trans.documents)
	msgsCount := len(trans.messages)
	lastMsg := trans.messages[msgsCount-1]
	trans.mu.Unlock()

	// Verify document was sent
	assert.GreaterOrEqual(t, docsCount, 1)

	// Verify success message with new subscription URL and portal link
	assert.Contains(t, lastMsg.Text, "New Universal Subscription URL")
	assert.Contains(t, lastMsg.Text, "Web Portal")
}

func TestBotEngine_DeviceWizard_Menu(t *testing.T) {
	userID := uuid.New()
	subToken := uuid.New().String()
	user := &store.User{
		ID:           userID,
		Username:     "testuser",
		Status:       pgtype.Text{String: "active", Valid: true},
		TrafficUsed:  pgtype.Int8{Int64: 1024, Valid: true},
		TrafficLimit: pgtype.Int8{Int64: 1048576, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}

	engine, trans, tgSrv, cpSrv := setupTestBotAndCP(t, user, subToken)
	defer tgSrv.Close()
	defer cpSrv.Close()

	ctx := context.Background()

	// Trigger /devices
	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 12345},
		Text: "/devices",
	}
	engine.handleMessage(ctx, msg)

	trans.mu.Lock()
	require.NotEmpty(t, trans.messages)
	lastMsg := trans.messages[len(trans.messages)-1]
	trans.mu.Unlock()

	assert.Contains(t, lastMsg.Text, "Select your platform")
	require.NotNil(t, lastMsg.ReplyMarkup)

	// Check that platforms are in the keyboard
	allData := make([]string, 0)
	for _, row := range lastMsg.ReplyMarkup.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData != nil {
				allData = append(allData, *btn.CallbackData)
			}
		}
	}
	assert.Contains(t, allData, "device:ios")
	assert.Contains(t, allData, "device:android")
	assert.Contains(t, allData, "device:windows")
	assert.Contains(t, allData, "device:macos")
	assert.Contains(t, allData, "device:linux")
}

func TestBotEngine_DeviceWizard_PlatformGuides(t *testing.T) {
	userID := uuid.New()
	subToken := uuid.New().String()
	user := &store.User{
		ID:           userID,
		Username:     "testuser",
		Status:       pgtype.Text{String: "active", Valid: true},
		TrafficUsed:  pgtype.Int8{Int64: 1024, Valid: true},
		TrafficLimit: pgtype.Int8{Int64: 1048576, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}

	engine, trans, tgSrv, cpSrv := setupTestBotAndCP(t, user, subToken)
	defer tgSrv.Close()
	defer cpSrv.Close()

	ctx := context.Background()

	platforms := []struct {
		device       string
		expectedLink string
		clientScheme string
	}{
		{device: "ios", expectedLink: "apps.apple.com", clientScheme: "happ://add/"},
		{device: "android", expectedLink: "play.google.com", clientScheme: "v2rayng://install-config"},
		{device: "windows", expectedLink: "github.com", clientScheme: "clash://install-config"},
		{device: "macos", expectedLink: "apps.apple.com", clientScheme: "streisand://import/"},
		{device: "linux", expectedLink: "github.com", clientScheme: "sing-box://import-remote-profile"},
	}

	for _, p := range platforms {
		cb := &tgbotapi.CallbackQuery{
			ID:   "cb_" + p.device,
			Data: "device:" + p.device,
			Message: &tgbotapi.Message{
				Chat: &tgbotapi.Chat{ID: 12345},
			},
		}
		engine.handleCallbackQuery(ctx, cb)

		trans.mu.Lock()
		lastMsg := trans.messages[len(trans.messages)-1]
		trans.mu.Unlock()

		assert.Contains(t, lastMsg.Text, p.clientScheme)
		require.NotNil(t, lastMsg.ReplyMarkup)

		// Check download button has expected link
		hasDownloadLink := false
		for _, row := range lastMsg.ReplyMarkup.InlineKeyboard {
			for _, btn := range row {
				if btn.URL != nil && strings.Contains(*btn.URL, p.expectedLink) {
					hasDownloadLink = true
				}
			}
		}
		assert.True(t, hasDownloadLink, "Missing download link for platform: "+p.device)
	}
}

func TestBotEngine_Referral(t *testing.T) {
	subToken := uuid.New().String()
	user := &store.User{
		ID:           uuid.New(),
		Username:     "alice",
		TrafficLimit: pgtype.Int8{Int64: 1048576, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}

	engine, trans, tgSrv, cpSrv := setupTestBotAndCP(t, user, subToken)
	defer tgSrv.Close()
	defer cpSrv.Close()

	ctx := context.Background()

	// 1. Test /ref command
	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 12345},
		Text: "/ref",
	}
	engine.handleMessage(ctx, msg)

	trans.mu.Lock()
	lastMsg := trans.messages[len(trans.messages)-1]
	trans.mu.Unlock()

	assert.Contains(t, lastMsg.Text, "ref_12345")
	require.NotNil(t, lastMsg.ReplyMarkup)

	// Check share button
	hasShareButton := false
	for _, row := range lastMsg.ReplyMarkup.InlineKeyboard {
		for _, btn := range row {
			if btn.URL != nil && strings.Contains(*btn.URL, "t.me/share/url") {
				hasShareButton = true
			}
		}
	}
	assert.True(t, hasShareButton, "Missing share button in referral message")

	// 2. Test action:referral callback
	cb := &tgbotapi.CallbackQuery{
		ID:   "cb_ref",
		Data: "action:referral",
		Message: &tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: 12345},
		},
	}
	engine.handleCallbackQuery(ctx, cb)

	trans.mu.Lock()
	lastCbMsg := trans.messages[len(trans.messages)-1]
	trans.mu.Unlock()

	assert.Contains(t, lastCbMsg.Text, "ref_12345")
}


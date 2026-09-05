package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/bot/client"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPClient_GetTrialPlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/plans/trial", r.URL.Path)
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))

		plan := store.Plan{
			ID:                 uuid.New(),
			Name:               "Free Trial 1GB",
			IsActive:           pgtype.Bool{Bool: true, Valid: true},
			IsTrial:            pgtype.Bool{Bool: true, Valid: true},
			TrialDurationHours: pgtype.Int4{Int32: 24, Valid: true},
			TrafficLimit:       pgtype.Int8{Int64: 1073741824, Valid: true},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(plan)
	}))
	defer server.Close()

	cp := client.NewCPClient(server.URL, "test-api-key")
	plan, err := cp.GetTrialPlan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Free Trial 1GB", plan.Name)
	assert.True(t, plan.IsTrial.Bool)
}

func TestCPClient_CreateTrial(t *testing.T) {
	subToken := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/users/trial", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		resp := client.TrialResponse{
			SubscriptionToken: subToken.String(),
			SubscriptionURL:   "/sub/" + subToken.String(),
			TrialHours:        24,
			TrafficLimitBytes: 1073741824,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cp := client.NewCPClient(server.URL, "test-api-key")
	res, err := cp.CreateTrial(context.Background(), 12345678, "alice_tg", "")
	require.NoError(t, err)
	assert.Equal(t, subToken.String(), res.SubscriptionToken)
	assert.Equal(t, int32(24), res.TrialHours)
}

func TestCPClient_RotateKeys(t *testing.T) {
	userID := uuid.New()
	newToken := uuid.New().String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/users/"+userID.String()+"/rotate", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		resp := client.RotateResponse{
			SubscriptionToken: newToken,
			SubscriptionURL:   "/sub/" + newToken,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cp := client.NewCPClient(server.URL, "test-api-key")
	res, err := cp.RotateKeys(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, newToken, res.SubscriptionToken)
}

func TestCPClient_GetSubscriptionConfig(t *testing.T) {
	token := uuid.New().String()
	mockConfig := "[Interface]\nPrivateKey = mock\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/sub/"+token, r.URL.Path)
		assert.Equal(t, "amneziawg", r.URL.Query().Get("format"))
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(mockConfig))
	}))
	defer server.Close()

	cp := client.NewCPClient(server.URL, "test-api-key")
	assert.Equal(t, server.URL, cp.BaseURL())

	cfg, err := cp.GetSubscriptionConfig(context.Background(), token, "amneziawg")
	require.NoError(t, err)
	assert.Equal(t, mockConfig, string(cfg))
}

func TestCPClient_CreateInvoice(t *testing.T) {
	orderID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/billing/invoices", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		resp := client.CreateInvoiceResponse{
			OrderID:           orderID,
			ExternalInvoiceID: "inv_12345",
			Amount:            "250",
			Currency:          "XTR",
			Gateway:           "stars",
			DurationMonths:    1,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cp := client.NewCPClient(server.URL, "test-api-key")
	inv, err := cp.CreateInvoice(context.Background(), client.CreateInvoiceRequest{
		UserID:         uuid.New(),
		PlanID:         uuid.New(),
		Gateway:        "stars",
		DurationMonths: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, orderID, inv.OrderID)
	assert.Equal(t, "stars", inv.Gateway)
}

func TestCPClient_GetBillingSettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/billing/settings", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))

		settings := client.BillingSettings{
			ID:                   1,
			CryptobotApiToken:    "test-tok",
			CryptobotEnabled:     true,
			TelegramStarsEnabled: true,
			StarsPricePerMonth:   250,
			WebhookSecret:        "secret123",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(settings)
	}))
	defer server.Close()

	cp := client.NewCPClient(server.URL, "test-api-key")
	s, err := cp.GetBillingSettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-tok", s.CryptobotApiToken)
	assert.True(t, s.CryptobotEnabled)
	assert.True(t, s.TelegramStarsEnabled)
	assert.Equal(t, int32(250), s.StarsPricePerMonth)
}

func TestCPClient_GetReferralStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/users/by-telegram/999888/referrals", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))

		res := client.ReferralStatsResponse{
			TelegramID:           999888,
			ReferralCode:         "ref_999888",
			ReferralCount:        4,
			BonusDaysPerReferral: 7,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	cp := client.NewCPClient(server.URL, "test-api-key")
	stats, err := cp.GetReferralStats(context.Background(), 999888)
	require.NoError(t, err)
	assert.Equal(t, int64(999888), stats.TelegramID)
	assert.Equal(t, "ref_999888", stats.ReferralCode)
	assert.Equal(t, int64(4), stats.ReferralCount)
	assert.Equal(t, 7, stats.BonusDaysPerReferral)
}



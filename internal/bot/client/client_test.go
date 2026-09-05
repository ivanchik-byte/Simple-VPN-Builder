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
		assert.Equal(t, "/api/v1/users/"+userID.String()+"/rotate-keys", r.URL.Path)
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

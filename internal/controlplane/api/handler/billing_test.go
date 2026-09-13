package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type mockBillingRepo struct {
	mu        sync.Mutex
	orders    map[uuid.UUID]store.Order
	gateways  map[string]store.PaymentGateway
	promos    map[string]store.PromoCode
	campaigns map[uuid.UUID]store.BroadcastCampaign
	settings  store.BillingSetting
}

func newMockBillingRepo() *mockBillingRepo {
	return &mockBillingRepo{
		orders:    make(map[uuid.UUID]store.Order),
		gateways:  make(map[string]store.PaymentGateway),
		promos:    make(map[string]store.PromoCode),
		campaigns: make(map[uuid.UUID]store.BroadcastCampaign),
		settings: store.BillingSetting{
			ID:                   1,
			CryptobotApiToken:    "",
			CryptobotEnabled:     true,
			TelegramStarsEnabled: true,
			StarsPricePerMonth:   250,
			WebhookSecret:        "",
		},
	}
}

func (m *mockBillingRepo) GetBillingSettings(_ context.Context) (store.BillingSetting, error) {
	return m.settings, nil
}

func (m *mockBillingRepo) UpsertBillingSettings(_ context.Context, params store.UpsertBillingSettingsParams) (store.BillingSetting, error) {
	m.settings = store.BillingSetting{
		ID:                   1,
		CryptobotApiToken:    params.CryptobotApiToken,
		CryptobotEnabled:     params.CryptobotEnabled,
		TelegramStarsEnabled: params.TelegramStarsEnabled,
		StarsPricePerMonth:   params.StarsPricePerMonth,
		WebhookSecret:        params.WebhookSecret,
	}
	return m.settings, nil
}


func (m *mockBillingRepo) CreateOrder(_ context.Context, params store.CreateOrderParams) (store.Order, error) {
	id := uuid.New()
	order := store.Order{
		ID:                id,
		UserID:            params.UserID,
		PlanID:            params.PlanID,
		Gateway:           params.Gateway,
		ExternalInvoiceID: params.ExternalInvoiceID,
		Amount:            params.Amount,
		Currency:          params.Currency,
		Status:            params.Status,
		DurationMonths:    params.DurationMonths,
	}
	m.orders[id] = order
	return order, nil
}

func (m *mockBillingRepo) GetOrderByID(_ context.Context, id uuid.UUID) (store.Order, error) {
	if o, ok := m.orders[id]; ok {
		return o, nil
	}
	return store.Order{}, errors.New("order not found")
}

func (m *mockBillingRepo) GetOrderByExternalInvoiceID(_ context.Context, extID string) (store.Order, error) {
	for _, o := range m.orders {
		if o.ExternalInvoiceID.Valid && o.ExternalInvoiceID.String == extID {
			return o, nil
		}
	}
	return store.Order{}, errors.New("order not found")
}

func (m *mockBillingRepo) UpdateOrderStatus(_ context.Context, id uuid.UUID, status string, paidAt *time.Time) (store.Order, error) {
	o, ok := m.orders[id]
	if !ok {
		return store.Order{}, errors.New("order not found")
	}
	o.Status = pgtype.Text{String: status, Valid: true}
	if paidAt != nil {
		o.PaidAt = pgtype.Timestamptz{Time: *paidAt, Valid: true}
	}
	m.orders[id] = o
	return o, nil
}

func (m *mockBillingRepo) ListOrdersByUserID(_ context.Context, userID uuid.UUID) ([]store.Order, error) {
	var list []store.Order
	for _, o := range m.orders {
		if o.UserID == userID {
			list = append(list, o)
		}
	}
	return list, nil
}

func (m *mockBillingRepo) GetPaymentGatewayByName(_ context.Context, name string) (store.PaymentGateway, error) {
	if gw, ok := m.gateways[name]; ok {
		return gw, nil
	}
	return store.PaymentGateway{}, errors.New("gateway not found")
}

func (m *mockBillingRepo) ListPaymentGateways(_ context.Context) ([]store.PaymentGateway, error) {
	var list []store.PaymentGateway
	for _, gw := range m.gateways {
		list = append(list, gw)
	}
	return list, nil
}

func (m *mockBillingRepo) UpsertPaymentGateway(_ context.Context, params store.UpsertPaymentGatewayParams) (store.PaymentGateway, error) {
	gw := store.PaymentGateway{
		Name:            params.Name,
		IsEnabled:       params.IsEnabled,
		ConfigEncrypted: params.ConfigEncrypted,
	}
	m.gateways[params.Name] = gw
	return gw, nil
}

func (m *mockBillingRepo) DeletePaymentGateway(_ context.Context, name string) error {
	delete(m.gateways, name)
	return nil
}

func (m *mockBillingRepo) GetPromoCode(_ context.Context, code string) (store.PromoCode, error) {
	if p, ok := m.promos[code]; ok {
		return p, nil
	}
	return store.PromoCode{}, errors.New("promo not found")
}

func (m *mockBillingRepo) IncrementPromoCodeUsage(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockBillingRepo) CreatePromoCode(_ context.Context, _ store.CreatePromoCodeParams) (store.PromoCode, error) {
	return store.PromoCode{}, nil
}

func (m *mockBillingRepo) ListPromoCodes(_ context.Context) ([]store.PromoCode, error) {
	return nil, nil
}

func (m *mockBillingRepo) CreateBroadcastCampaign(_ context.Context, params store.CreateBroadcastCampaignParams) (store.BroadcastCampaign, error) {
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

func (m *mockBillingRepo) GetBroadcastCampaign(_ context.Context, id uuid.UUID) (store.BroadcastCampaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.campaigns[id]; ok {
		return c, nil
	}
	return store.BroadcastCampaign{}, errors.New("campaign not found")
}

func (m *mockBillingRepo) UpdateBroadcastCampaignStats(_ context.Context, params store.UpdateBroadcastCampaignStatsParams) (store.BroadcastCampaign, error) {
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

func (m *mockBillingRepo) ListBroadcastCampaigns(_ context.Context) ([]store.BroadcastCampaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var list []store.BroadcastCampaign
	for _, c := range m.campaigns {
		list = append(list, c)
	}
	return list, nil
}

func (m *mockBillingRepo) GetBotReplies(_ context.Context) (map[string]string, error) {
	return map[string]string{
		"welcome_new_user": "Welcome",
	}, nil
}

func (m *mockBillingRepo) UpsertBotReply(_ context.Context, _, _ string) error {
	return nil
}

func TestBillingHandler_Webhook_HMACVerification(t *testing.T) {
	billingRepo := newMockBillingRepo()
	userRepo := newMockUserRepo()
	planRepo := newMockPlanRepo()
	handler := NewBillingHandler(billingRepo, userRepo, planRepo, nil)

	r := chi.NewRouter()
	r.Post("/api/v1/billing/webhooks/{gateway}", handler.ProcessWebhook)

	secretToken := "secret-cryptobot-token-123"
	billingRepo.gateways["cryptobot"] = store.PaymentGateway{
		Name:            "cryptobot",
		IsEnabled:       pgtype.Bool{Bool: true, Valid: true},
		ConfigEncrypted: `{"token":"` + secretToken + `"}`,
	}

	userID := uuid.New()
	userRepo.users[userID] = store.User{
		ID:        userID,
		Username:  "payuser",
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}

	planID := uuid.New()
	planRepo.plans[planID] = store.Plan{
		ID:           planID,
		Name:         "Standard",
		TrafficLimit: pgtype.Int8{Int64: 50 * 1024 * 1024 * 1024, Valid: true},
	}

	extInvoiceID := "inv_crypto_test_01"
	orderID := uuid.New()
	billingRepo.orders[orderID] = store.Order{
		ID:                orderID,
		UserID:            userID,
		PlanID:            planID,
		Gateway:           "cryptobot",
		ExternalInvoiceID: pgtype.Text{String: extInvoiceID, Valid: true},
		Status:            pgtype.Text{String: "pending", Valid: true},
		DurationMonths:    pgtype.Int4{Int32: 1, Valid: true},
	}

	payload := map[string]string{
		"external_invoice_id": extInvoiceID,
		"status":              "paid",
	}
	bodyBytes, _ := json.Marshal(payload)

	t.Run("Valid HMAC signature from CryptoBot succeeds", func(t *testing.T) {
		tokenHash := sha256.Sum256([]byte(secretToken))
		mac := hmac.New(sha256.New, tokenHash[:])
		mac.Write(bodyBytes)
		validSig := hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/webhooks/cryptobot", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("crypto-pay-api-signature", validSig)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		updatedOrder := billingRepo.orders[orderID]
		assert.Equal(t, "paid", updatedOrder.Status.String)
	})

	t.Run("Invalid HMAC signature is rejected with 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/webhooks/cryptobot", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("crypto-pay-api-signature", "invalid-fake-signature")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("Disabled gateway is rejected with 403", func(t *testing.T) {
		billingRepo.gateways["disabled_gw"] = store.PaymentGateway{
			Name:      "disabled_gw",
			IsEnabled: pgtype.Bool{Bool: false, Valid: true},
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/webhooks/disabled_gw", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("Idempotent webhook does not error on already paid order", func(t *testing.T) {
		tokenHash := sha256.Sum256([]byte(secretToken))
		mac := hmac.New(sha256.New, tokenHash[:])
		mac.Write(bodyBytes)
		validSig := hex.EncodeToString(mac.Sum(nil))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/webhooks/cryptobot", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("crypto-pay-api-signature", validSig)
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		assert.Equal(t, "already_processed", resp["status"])
	})
}

func TestBillingHandler_DynamicPricing(t *testing.T) {
	billingRepo := newMockBillingRepo()
	userRepo := newMockUserRepo()
	planRepo := newMockPlanRepo()
	handler := NewBillingHandler(billingRepo, userRepo, planRepo, nil)

	r := chi.NewRouter()
	r.Post("/api/v1/billing/invoices", handler.CreateInvoice)

	userID := uuid.New()
	userRepo.users[userID] = store.User{
		ID:       userID,
		Username: "buyer",
	}

	planID := uuid.New()
	var priceNum pgtype.Numeric
	_ = priceNum.Scan("10.00")
	planRepo.plans[planID] = store.Plan{
		ID:           planID,
		Name:         "Pro Plan",
		MonthlyPrice: priceNum,
		PriceStars:   pgtype.Int4{Int32: 100, Valid: true},
	}

	// 15% discount promo code
	billingRepo.promos["SUMMER15"] = store.PromoCode{
		ID:              uuid.New(),
		Code:            "SUMMER15",
		DiscountPercent: pgtype.Int4{Int32: 15, Valid: true},
		IsActive:        pgtype.Bool{Bool: true, Valid: true},
	}

	t.Run("1 Month at full price", func(t *testing.T) {
		reqBody, _ := json.Marshal(CreateInvoiceRequest{
			UserID:         userID,
			PlanID:         planID,
			Gateway:        "cryptobot",
			DurationMonths: 1,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/invoices", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)

		var resp CreateInvoiceResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		assert.Equal(t, "10.00", resp.Amount)
	})

	t.Run("12 Months with bulk discount (20%) and promo code (15%)", func(t *testing.T) {
		// Base: 10 * 12 = 120
		// 12m bulk discount: 120 * 0.80 = 96.00
		// Promo 15%: 96.00 * 0.85 = 81.60
		reqBody, _ := json.Marshal(CreateInvoiceRequest{
			UserID:         userID,
			PlanID:         planID,
			Gateway:        "cryptobot",
			DurationMonths: 12,
			PromoCode:      "SUMMER15",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/invoices", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)

		var resp CreateInvoiceResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		assert.Equal(t, "81.60", resp.Amount)
	})

	t.Run("Stars pricing with bulk discount", func(t *testing.T) {
		// Base: 100 stars * 12 months = 1200
		// 12m bulk discount (20%): 1200 * 0.80 = 960 stars
		reqBody, _ := json.Marshal(CreateInvoiceRequest{
			UserID:         userID,
			PlanID:         planID,
			Gateway:        "stars",
			DurationMonths: 12,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/invoices", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)

		var resp CreateInvoiceResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		assert.Equal(t, "960", resp.Amount)
		assert.Equal(t, "XTR", resp.Currency)
	})
}

func TestBillingHandler_SettingsAPI(t *testing.T) {
	billingRepo := newMockBillingRepo()
	userRepo := newMockUserRepo()
	planRepo := newMockPlanRepo()
	handler := NewBillingHandler(billingRepo, userRepo, planRepo, nil)

	r := chi.NewRouter()
	r.Get("/api/v1/billing/settings", handler.GetSettings)
	r.Put("/api/v1/billing/settings", handler.UpdateSettings)

	// 1. Get initial settings
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing/settings", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var settings store.BillingSetting
	err := json.Unmarshal(rec.Body.Bytes(), &settings)
	require.NoError(t, err)
	assert.True(t, settings.TelegramStarsEnabled)
	assert.Equal(t, int32(250), settings.StarsPricePerMonth)

	// 2. Update settings
	updateBody, _ := json.Marshal(UpdateBillingSettingsRequest{
		CryptobotApiToken:    "test-secret-token",
		CryptobotEnabled:     true,
		TelegramStarsEnabled: false,
		StarsPricePerMonth:   300,
		WebhookSecret:        "test-webhook-secret",
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/billing/settings", bytes.NewReader(updateBody))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var updated store.BillingSetting
	err = json.Unmarshal(rec.Body.Bytes(), &updated)
	require.NoError(t, err)
	assert.Equal(t, "test-secret-token", updated.CryptobotApiToken)
	assert.True(t, updated.CryptobotEnabled)
	assert.False(t, updated.TelegramStarsEnabled)
	assert.Equal(t, int32(300), updated.StarsPricePerMonth)
	assert.Equal(t, "test-webhook-secret", updated.WebhookSecret)
}

func TestBillingHandler_GatewayDisableEnforcement(t *testing.T) {
	billingRepo := newMockBillingRepo()
	userRepo := newMockUserRepo()
	planRepo := newMockPlanRepo()
	handler := NewBillingHandler(billingRepo, userRepo, planRepo, nil)

	r := chi.NewRouter()
	r.Post("/api/v1/billing/invoices", handler.CreateInvoice)
	r.Post("/api/v1/billing/webhooks/{gateway}", handler.ProcessWebhook)

	userID := uuid.New()
	userRepo.users[userID] = store.User{ID: userID, Username: "testuser"}
	planID := uuid.New()
	var priceNum pgtype.Numeric
	_ = priceNum.Scan("10.00")
	planRepo.plans[planID] = store.Plan{
		ID:           planID,
		Name:         "Standard",
		MonthlyPrice: priceNum,
	}

	// Disable CryptoBot
	billingRepo.settings.CryptobotEnabled = false
	billingRepo.gateways["cryptobot"] = store.PaymentGateway{
		Name:      "cryptobot",
		IsEnabled: pgtype.Bool{Bool: false, Valid: true},
	}

	// 1. CreateInvoice cryptobot should fail with 400
	reqBody, _ := json.Marshal(CreateInvoiceRequest{
		UserID:         userID,
		PlanID:         planID,
		Gateway:        "cryptobot",
		DurationMonths: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/invoices", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "disabled")

	// 2. Webhook for disabled cryptobot should fail with 403
	req = httptest.NewRequest(http.MethodPost, "/api/v1/billing/webhooks/cryptobot", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "disabled")
}

func TestBillingHandler_BroadcastAPI(t *testing.T) {
	billingRepo := newMockBillingRepo()
	userRepo := newMockUserRepo()
	planRepo := newMockPlanRepo()
	handler := NewBillingHandler(billingRepo, userRepo, planRepo, nil)

	// Create broadcast service with fast delay
	broadcastSvc := service.NewBroadcastService(billingRepo, userRepo, nil, nil)
	broadcastSvc.SetRateDelay(1 * time.Millisecond)
	handler.SetBroadcastService(broadcastSvc)

	r := chi.NewRouter()
	r.Post("/api/v1/broadcasts", handler.CreateBroadcast)
	r.Get("/api/v1/broadcasts", handler.ListBroadcasts)
	r.Get("/api/v1/broadcasts/{id}", handler.GetBroadcast)

	// Add mock telegram user
	u, _ := userRepo.Create(context.Background(), store.CreateUserParams{
		Username: "tguser1",
	})
	_, _ = userRepo.UpdateTelegramMetadata(context.Background(), u.ID, 999111, "tguser1", false, nil, "ref_999111")

	// 1. Create Broadcast
	body, _ := json.Marshal(CreateBroadcastRequest{
		Title:         "System Upgrade",
		TargetSegment: "all",
		MessageText:   "Network scheduled maintenance in 1 hour.",
		Buttons: []service.BroadcastButton{
			{Text: "Status", URL: "https://status.example.com"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/broadcasts", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var createdCampaign store.BroadcastCampaign
	err := json.Unmarshal(rec.Body.Bytes(), &createdCampaign)
	require.NoError(t, err)
	assert.Equal(t, "System Upgrade", createdCampaign.Title)

	// 2. List Broadcasts
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/broadcasts", nil)
	recList := httptest.NewRecorder()
	r.ServeHTTP(recList, reqList)
	assert.Equal(t, http.StatusOK, recList.Code)
	assert.Contains(t, recList.Body.String(), "System Upgrade")

	// 3. Get Broadcast by ID
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/broadcasts/"+createdCampaign.ID.String(), nil)
	recGet := httptest.NewRecorder()
	r.ServeHTTP(recGet, reqGet)
	assert.Equal(t, http.StatusOK, recGet.Code)
	assert.Contains(t, recGet.Body.String(), "System Upgrade")
}



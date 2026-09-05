package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeb_TemplateEngine(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	rec := httptest.NewRecorder()
	err = engine.Render(rec, "login.html", map[string]any{
		"IsLoginPage": true,
		"Error":       "",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Control Plane")
	assert.Contains(t, rec.Body.String(), "Admin Access")
}

func TestWeb_TemplateEngine_HelpersAndPortal(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	rec := httptest.NewRecorder()
	err = engine.RenderStandalone(rec, "portal.html", map[string]any{
		"User": store.User{
			Username:     "alice",
			Status:       pgtype.Text{String: "active", Valid: true},
			TrafficUsed:  pgtype.Int8{Int64: 524288000, Valid: true},
			TrafficLimit: pgtype.Int8{Int64: 1073741824, Valid: true},
		},
		"Plan": map[string]any{
			"Name":        "Pro VPN",
			"Bandwidth":   1000000000,
			"DeviceLimit": 5,
		},
		"SubToken":        "test-token-uuid-12345",
		"SubURL":          "http://localhost:8110/sub/test-token-uuid-12345",
		"ActiveClients":   2,
		"TrafficUsed":     pgtype.Int8{Int64: 524288000, Valid: true},
		"TrafficLimit":    pgtype.Int8{Int64: 1073741824, Valid: true},
		"QuotaPercent":    48.8,
		"TrafficLeft":     "524.0 MB",
		"DaysRemaining":   29,
		"ExpiresAt":       time.Now().Add(29 * 24 * time.Hour),
		"SingboxImport":   "sing-box://import-remote-profile?url=http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345%3Fformat%3Dsingbox",
		"ClashImport":     "clash://install-config?url=http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345%3Fformat%3Dclash",
		"HiddifyImport":   "hiddify://install-sub?url=http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345",
		"StreisandImport": "streisand://import/http%3A%2F%2Flocalhost%3A8110%2Fsub%2Ftest-token-uuid-12345",
		"QRCodeDataURL":   "data:image/svg+xml;utf8,<svg></svg>",
		"PrimaryOS":       "unknown",
		"Nodes":           []any{},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "alice")
	assert.Contains(t, body, "Bandwidth Consumed")
	assert.Contains(t, body, "test-token-uuid-12345")
	assert.Contains(t, body, "Interactive Device Setup")
	assert.Contains(t, body, "happ://add/")
	assert.Contains(t, body, "v2rayng://install-config")
	assert.Contains(t, body, "streisand://import/")
	assert.Contains(t, body, "sing-box://import-remote-profile")
	assert.Contains(t, body, "Regenerate Keys / Reset Token")
}

func TestWeb_ReadHostTelemetry(t *testing.T) {
	cpuPercent, cpuModel, ramUsed, ramTotal, diskUsed, diskTotal := ReadHostTelemetry()
	assert.GreaterOrEqual(t, cpuPercent, 0.0)
	assert.NotEmpty(t, cpuModel)
	assert.GreaterOrEqual(t, ramTotal, int64(0))
	assert.GreaterOrEqual(t, ramUsed, int64(0))
	assert.GreaterOrEqual(t, diskTotal, int64(0))
	assert.GreaterOrEqual(t, diskUsed, int64(0))

	rxRate, txRate := ReadHostNetworkRates()
	assert.GreaterOrEqual(t, rxRate, int64(0))
	assert.GreaterOrEqual(t, txRate, int64(0))
}

func TestWeb_RequireWebAuth_Redirect(t *testing.T) {
	handler := RequireWebAuth(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/admin/login", rec.Header().Get("Location"))
}

type mockWebUserRepo struct {
	store.UserRepository
	user store.User
	err  error
}

func (m *mockWebUserRepo) GetBySubscriptionToken(_ context.Context, _ uuid.UUID) (store.User, error) {
	if m.err != nil {
		return store.User{}, m.err
	}
	return m.user, nil
}

func (m *mockWebUserRepo) RotateSubscriptionToken(_ context.Context, _ uuid.UUID) (store.User, error) {
	if m.err != nil {
		return store.User{}, m.err
	}
	m.user.SubscriptionToken = uuid.New()
	return m.user, nil
}

func TestRotateClientCredentials_Success(t *testing.T) {
	token := uuid.New()
	initialUser := store.User{
		ID:                uuid.New(),
		Username:          "bob",
		SubscriptionToken: token,
		Status:            pgtype.Text{String: "active", Valid: true},
	}

	repos := &store.Repositories{
		Users: &mockWebUserRepo{user: initialUser},
	}
	h := &Handler{repos: repos}

	// 1. JSON Request
	req := httptest.NewRequest(http.MethodPost, "/client/"+token.String()+"/rotate", nil)
	req.Header.Set("Accept", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.RotateClientCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["new_token"])
	assert.Contains(t, resp["redirect_url"], "/client/")

	// 2. Form Request (Redirect)
	reqForm := httptest.NewRequest(http.MethodPost, "/client/"+token.String()+"/rotate", nil)
	rctxForm := chi.NewRouteContext()
	rctxForm.URLParams.Add("token", token.String())
	reqForm = reqForm.WithContext(context.WithValue(reqForm.Context(), chi.RouteCtxKey, rctxForm))

	recForm := httptest.NewRecorder()
	h.RotateClientCredentials(recForm, reqForm)

	assert.Equal(t, http.StatusSeeOther, recForm.Code)
	assert.Contains(t, recForm.Header().Get("Location"), "/client/")
}

func TestRotateClientCredentials_NotFound(t *testing.T) {
	repos := &store.Repositories{
		Users: &mockWebUserRepo{err: fmt.Errorf("user not found")},
	}
	h := &Handler{repos: repos}

	token := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/client/"+token.String()+"/rotate", nil)
	req.Header.Set("Accept", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.RotateClientCredentials(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestConnectDeepLink(t *testing.T) {
	token := uuid.New()
	user := store.User{
		ID:                uuid.New(),
		Username:          "charlie",
		SubscriptionToken: token,
		Status:            pgtype.Text{String: "active", Valid: true},
	}

	repos := &store.Repositories{
		Users: &mockWebUserRepo{user: user},
	}
	h := &Handler{repos: repos}

	tests := []struct {
		client       string
		expectedLink string
	}{
		{client: "happ", expectedLink: "happ://add/"},
		{client: "v2rayng", expectedLink: "v2rayng://install-config"},
		{client: "streisand", expectedLink: "streisand://import/"},
		{client: "singbox", expectedLink: "sing-box://import-remote-profile"},
		{client: "clash", expectedLink: "clash://install-config"},
		{client: "hiddify", expectedLink: "hiddify://install-sub"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, "/client/"+token.String()+"/connect?client="+tc.client, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("token", token.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		h.ConnectDeepLink(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), tc.expectedLink)
		assert.Contains(t, rec.Body.String(), "Launching VPN Profile")
	}

	// Unknown client redirects to portal
	reqUnknown := httptest.NewRequest(http.MethodGet, "/client/"+token.String()+"/connect?client=unknown", nil)
	rctxUnknown := chi.NewRouteContext()
	rctxUnknown.URLParams.Add("token", token.String())
	reqUnknown = reqUnknown.WithContext(context.WithValue(reqUnknown.Context(), chi.RouteCtxKey, rctxUnknown))

	recUnknown := httptest.NewRecorder()
	h.ConnectDeepLink(recUnknown, reqUnknown)

	assert.Equal(t, http.StatusSeeOther, recUnknown.Code)
	assert.Equal(t, "/client/"+token.String(), recUnknown.Header().Get("Location"))
}

func TestWeb_TemplateEngine_SettingsBilling(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	rec := httptest.NewRecorder()
	err = engine.Render(rec, "settings.html", map[string]any{
		"ActiveNav":  "billing",
		"AdminUser":  "admin@example.com",
		"Admins":     []store.Admin{},
		"APIKeys":    []store.ApiKey{},
		"Gateways":   []store.PaymentGateway{},
		"ActiveTab":  "billing",
		"BillingSettings": store.BillingSetting{
			CryptobotApiToken:    "test-crypto-token",
			CryptobotEnabled:     true,
			TelegramStarsEnabled: true,
			StarsPricePerMonth:   250,
			WebhookSecret:        "test-secret",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Billing")
	assert.Contains(t, body, "Payment Gateways")
	assert.Contains(t, body, "cryptobot_api_token")
	assert.Contains(t, body, "telegram_stars_enabled")
	assert.Contains(t, body, "stars_price_per_month")
	assert.Contains(t, body, "Save Billing Settings")
}

func TestWeb_TemplateEngine_PlansBuilder(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	var p1m, p3m, p6m, p12m pgtype.Numeric
	_ = p1m.Scan("9.99")
	_ = p3m.Scan("26.99")
	_ = p6m.Scan("49.99")
	_ = p12m.Scan("89.99")

	plan := store.Plan{
		ID:             uuid.New(),
		Name:           "Pro Ultimate",
		MaxDevices:     pgtype.Int4{Int32: 5, Valid: true},
		TrafficLimitGb: pgtype.Int4{Int32: 250, Valid: true},
		Price1m:        p1m,
		Price3m:        p3m,
		Price6m:        p6m,
		Price12m:       p12m,
		Protocols:      []string{"wireguard", "amneziawg", "vless"},
		IsActive:       pgtype.Bool{Bool: true, Valid: true},
	}

	rec := httptest.NewRecorder()
	err = engine.Render(rec, "plans.html", map[string]any{
		"ActiveNav": "plans",
		"AdminUser": "admin@example.com",
		"Plans":     []store.Plan{plan},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Pro Ultimate")
	assert.Contains(t, body, "wireguard")
	assert.Contains(t, body, "amneziawg")
	assert.Contains(t, body, "vless")
	assert.Contains(t, body, "250 GB")
	assert.Contains(t, body, "5 dev")
	assert.Contains(t, body, "9.99")
}

func TestWeb_TemplateEngine_Broadcast(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	campaign := store.BroadcastCampaign{
		ID:              uuid.New(),
		Title:           "Spring Promo",
		TargetSegment:   "all",
		MessageText:     "Special discount for all users",
		TotalRecipients: pgtype.Int4{Int32: 100, Valid: true},
		SentCount:       pgtype.Int4{Int32: 95, Valid: true},
		FailedCount:     pgtype.Int4{Int32: 5, Valid: true},
		Status:          pgtype.Text{String: "completed", Valid: true},
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	rec := httptest.NewRecorder()
	err = engine.Render(rec, "broadcast.html", map[string]any{
		"ActiveNav": "broadcast",
		"AdminUser": "admin@example.com",
		"Campaigns": []store.BroadcastCampaign{campaign},
		"Sent":      true,
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Compose Campaign")
	assert.Contains(t, body, "Spring Promo")
	assert.Contains(t, body, "COMPLETED")
	assert.Contains(t, body, "95 / 100")
}



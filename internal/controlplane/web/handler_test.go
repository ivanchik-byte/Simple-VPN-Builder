package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pquerna/otp/totp"
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

	recDash := httptest.NewRecorder()
	errDash := engine.Render(recDash, "dashboard.html", map[string]any{
		"Theme":     "dark",
		"ActiveNav": "dashboard",
		"Nodes": []store.Node{
			{
				ID:            uuid.New(),
				Name:          "Frankfurt-01",
				Endpoint:      "1.2.3.4:51820",
				Region:        pgtype.Text{String: "eu-central", Valid: true},
				Status:        pgtype.Text{String: "online", Valid: true},
				LastHeartbeat: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			},
		},
		"Telemetry": TelemetryData{},
		"Stats": StatsSummary{
			ActiveNodes:        1,
			TotalNodes:         1,
			ActiveUsers:        0,
			TotalUsers:         0,
			TotalTrafficBytes:  0,
			SupportedProtocols: 3,
		},
	})
	require.NoError(t, errDash)
	assert.Equal(t, http.StatusOK, recDash.Code)
	assert.Contains(t, recDash.Body.String(), "Frankfurt-01")
	assert.Contains(t, recDash.Body.String(), "1.2.3.4:51820")

	recNode := httptest.NewRecorder()
	errNode := engine.Render(recNode, "node_detail.html", map[string]any{
		"Theme":     "dark",
		"ActiveNav": "nodes",
		"Node": store.Node{
			ID:            uuid.New(),
			Name:          "Frankfurt-01",
			Endpoint:      "1.2.3.4:51820",
			GrpcEndpoint:  "1.2.3.4:9090",
			Region:        pgtype.Text{String: "eu-central", Valid: true},
			Status:        pgtype.Text{String: "online", Valid: true},
			PublicKey:     "dummy-pubkey-123",
			LastHeartbeat: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		},
		"Telemetry":   TelemetryData{},
		"Credentials": []store.Credential{},
	})
	require.NoError(t, errNode)
	assert.Equal(t, http.StatusOK, recNode.Code)
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
	assert.Contains(t, body, "webhook_secret")
	assert.Contains(t, body, "Save Billing Settings")
}

func TestWeb_TemplateEngine_SettingsAdmins(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	ownerAdmin := store.Admin{
		ID:        uuid.New(),
		Email:     "owner@vpnbuilder.local",
		Role:      pgtype.Text{String: "owner", Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	superAdmin := store.Admin{
		ID:        uuid.New(),
		Email:     "super@vpnbuilder.local",
		Role:      pgtype.Text{String: "superadmin", Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Hour), Valid: true},
	}
	adminUser := store.Admin{
		ID:        uuid.New(),
		Email:     "regular@vpnbuilder.local",
		Role:      pgtype.Text{String: "admin", Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: time.Now().Add(-2 * time.Hour), Valid: true},
	}

	apiKey := store.ApiKey{
		ID:         uuid.New(),
		Name:       "Seller-Bot",
		Prefix:     "vpn_065a",
		Scopes:     []string{"admin"},
		CreatedAt:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
		LastUsedAt: pgtype.Timestamptz{Valid: false},
	}

	// Test viewing as Owner:
	// - Owner shows Protected
	// - Superadmin shows Delete
	// - Admin shows Delete
	rec1 := httptest.NewRecorder()
	err = engine.Render(rec1, "settings.html", map[string]any{
		"ActiveNav":      "settings",
		"ActiveTab":      "settings",
		"CurrentAdminID": ownerAdmin.ID.String(),
		"CurrentRole":    "owner",
		"Admins":         []store.Admin{ownerAdmin, superAdmin, adminUser},
		"APIKeys":        []store.ApiKey{apiKey},
		"TotpEnabled":    false,
		"TOTPSecret":     "JBSWY3DPEHPK3PXP",
		"TOTPOTPURL":     "otpauth://totp/Simple-VPN-Builder:owner@vpnbuilder.local",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec1.Code)
	body1 := rec1.Body.String()
	assert.Contains(t, body1, "System Administrators")
	assert.Contains(t, body1, "You (Active)")
	assert.Contains(t, body1, "Delete")
	assert.Contains(t, body1, "Setup 2FA Now")
	assert.Contains(t, body1, "JBSWY3DPEHPK3PXP")
	assert.Contains(t, body1, "vpn_065a...")
	assert.Contains(t, body1, "create-admin-modal")
	assert.Contains(t, body1, "create-key-modal")
	assert.Contains(t, body1, "setup-2fa-modal")
	assert.Contains(t, body1, "permissions-modal")
	assert.Contains(t, body1, "openPermissionsModal")

	adminUser2 := store.Admin{
		ID:        uuid.New(),
		Email:     "another@vpnbuilder.local",
		Role:      pgtype.Text{String: "admin", Valid: true},
		CreatedAt: pgtype.Timestamptz{Time: time.Now().Add(-3 * time.Hour), Valid: true},
	}

	// Test viewing as Regular Admin:
	// - All Delete buttons are hidden ("No Access", "Owner Only", or "Protected")
	rec2 := httptest.NewRecorder()
	err = engine.Render(rec2, "settings.html", map[string]any{
		"ActiveNav":      "settings",
		"ActiveTab":      "settings",
		"CurrentAdminID": adminUser.ID.String(),
		"CurrentRole":    "admin",
		"Admins":         []store.Admin{ownerAdmin, superAdmin, adminUser, adminUser2},
		"APIKeys":        []store.ApiKey{},
		"TotpEnabled":    true,
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec2.Code)
	body2 := rec2.Body.String()
	assert.Contains(t, body2, "Owner (Protected)")
	assert.Contains(t, body2, "No Access")
	assert.Contains(t, body2, "Disable 2FA")
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

type mockWebAdminRepo struct {
	store.AdminRepository
	admins map[uuid.UUID]store.Admin
}

func (m *mockWebAdminRepo) Create(_ context.Context, params store.CreateAdminParams) (store.Admin, error) {
	id := uuid.New()
	a := store.Admin{
		ID:           id,
		Email:        params.Email,
		PasswordHash: params.PasswordHash,
		Role:         params.Role,
	}
	m.admins[id] = a
	return a, nil
}

func (m *mockWebAdminRepo) GetByID(_ context.Context, id uuid.UUID) (store.Admin, error) {
	if a, ok := m.admins[id]; ok {
		return a, nil
	}
	return store.Admin{}, fmt.Errorf("admin not found")
}

func (m *mockWebAdminRepo) Update(_ context.Context, params store.UpdateAdminParams) (store.Admin, error) {
	a, ok := m.admins[params.ID]
	if !ok {
		return store.Admin{}, fmt.Errorf("admin not found")
	}
	a.Email = params.Email
	a.Role = params.Role
	a.TotpSecret = params.TotpSecret
	m.admins[params.ID] = a
	return a, nil
}

func (m *mockWebAdminRepo) List(_ context.Context) ([]store.Admin, error) {
	list := make([]store.Admin, 0, len(m.admins))
	for _, a := range m.admins {
		list = append(list, a)
	}
	return list, nil
}

func (m *mockWebAdminRepo) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.admins[id]; !ok {
		return fmt.Errorf("admin not found")
	}
	delete(m.admins, id)
	return nil
}

func TestRoleHierarchy_DeleteAdmin(t *testing.T) {
	ownerID := uuid.New()
	superID := uuid.New()
	adminID := uuid.New()

	adminsMap := map[uuid.UUID]store.Admin{
		ownerID: {ID: ownerID, Email: "owner@vpn.test", Role: pgtype.Text{String: "owner", Valid: true}},
		superID: {ID: superID, Email: "super@vpn.test", Role: pgtype.Text{String: "superadmin", Valid: true}},
		adminID: {ID: adminID, Email: "admin@vpn.test", Role: pgtype.Text{String: "admin", Valid: true}},
	}
	adminRepo := &mockWebAdminRepo{admins: adminsMap}
	repos := &store.Repositories{Admins: adminRepo}
	h := &Handler{repos: repos}

	callDelete := func(callerID uuid.UUID, callerRole string, targetID uuid.UUID) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/admin/admins/"+targetID.String()+"/delete", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", targetID.String())
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		r = r.WithContext(context.WithValue(r.Context(), AdminContextKey, &AdminContext{
			AdminID:  callerID,
			Username: "caller",
			Role:     callerRole,
		}))
		rec := httptest.NewRecorder()
		h.DeleteAdmin(rec, r)
		return rec
	}

	// 1. Owner cannot delete themselves
	rec := callDelete(ownerID, "owner", ownerID)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "error=You+cannot+delete+your+own+account")

	// 2. Superadmin cannot delete Owner
	rec = callDelete(superID, "superadmin", ownerID)
	assert.Contains(t, rec.Header().Get("Location"), "error=Forbidden:+only+an+Owner+can+delete+another+Owner+account")

	// 3. Superadmin cannot delete another Superadmin
	anotherSuperID := uuid.New()
	adminRepo.admins[anotherSuperID] = store.Admin{ID: anotherSuperID, Role: pgtype.Text{String: "superadmin", Valid: true}}
	rec = callDelete(superID, "superadmin", anotherSuperID)
	assert.Contains(t, rec.Header().Get("Location"), "error=Superadmins+can+only+be+deleted+by+the+Owner")

	// 4. Regular Admin cannot delete anyone
	rec = callDelete(adminID, "admin", adminID)
	assert.Contains(t, rec.Header().Get("Location"), "error=Forbidden:+insufficient+privileges")

	// 5. Superadmin CAN delete regular Admin
	rec = callDelete(superID, "superadmin", adminID)
	assert.Contains(t, rec.Header().Get("Location"), "success=Administrator+deleted+successfully")
	_, exists := adminRepo.admins[adminID]
	assert.False(t, exists)

	// 6. Owner CAN delete Superadmin
	rec = callDelete(ownerID, "owner", superID)
	assert.Contains(t, rec.Header().Get("Location"), "success=Administrator+deleted+successfully")
	_, exists = adminRepo.admins[superID]
	assert.False(t, exists)

	// 7. Owner CANNOT delete the sole remaining Owner
	rec = callDelete(ownerID, "owner", ownerID)
	assert.Contains(t, rec.Header().Get("Location"), "error=You+cannot+delete+your+own+account")

	secondOwnerID := uuid.New()
	adminRepo.admins[secondOwnerID] = store.Admin{ID: secondOwnerID, Email: "second_owner@vpn.test", Role: pgtype.Text{String: "owner", Valid: true}}

	// 8. Owner CAN delete another Owner when >= 2 owners exist
	rec = callDelete(ownerID, "owner", secondOwnerID)
	assert.Contains(t, rec.Header().Get("Location"), "success=Administrator+deleted+successfully")
	_, exists = adminRepo.admins[secondOwnerID]
	assert.False(t, exists)

	// 9. When only 1 owner remains, attempting to delete it fails
	// Simulate an external caller with role owner attempting to delete ownerID
	otherOwnerCallerID := uuid.New()
	rec = callDelete(otherOwnerCallerID, "owner", ownerID)
	assert.Contains(t, rec.Header().Get("Location"), "error=Cannot+delete+the+sole+remaining+Owner")
}

func TestWeb_TOTP_EnableAndDisable(t *testing.T) {
	adminID := uuid.New()
	adminRepo := &mockWebAdminRepo{
		admins: map[uuid.UUID]store.Admin{
			adminID: {ID: adminID, Email: "totp@vpn.test", Role: pgtype.Text{String: "owner", Valid: true}},
		},
	}
	totpMgr := auth.NewTOTPManager("Simple-VPN-Builder")
	repos := &store.Repositories{Admins: adminRepo}
	h := &Handler{repos: repos, totpManager: totpMgr}

	secret, _, err := totpMgr.GenerateSecret("totp@vpn.test")
	require.NoError(t, err)

	// 1. Invalid code is rejected
	req := httptest.NewRequest(http.MethodPost, "/admin/2fa/enable?secret="+secret+"&code=000000", nil)
	req = req.WithContext(context.WithValue(req.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "owner"}))
	rec := httptest.NewRecorder()
	h.EnableTOTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "error=Invalid+2FA+passcode")

	// 2. Valid code enables 2FA
	now := time.Now().UTC()
	validCode := totpMgr.ValidateCode("", secret)
	assert.False(t, validCode)
	code, err := totp.GenerateCode(secret, now)
	require.NoError(t, err)
	reqValid := httptest.NewRequest(http.MethodPost, "/admin/2fa/enable?secret="+secret+"&code="+code, nil)
	reqValid = reqValid.WithContext(context.WithValue(reqValid.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "owner"}))
	recValid := httptest.NewRecorder()
	h.EnableTOTP(recValid, reqValid)
	assert.Equal(t, http.StatusSeeOther, recValid.Code)
	assert.Contains(t, recValid.Header().Get("Location"), "success=")
	assert.True(t, adminRepo.admins[adminID].TotpSecret.Valid)

	// 3. Disable 2FA clears TotpSecret
	reqDisable := httptest.NewRequest(http.MethodPost, "/admin/2fa/disable", nil)
	reqDisable = reqDisable.WithContext(context.WithValue(reqDisable.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "owner"}))
	recDisable := httptest.NewRecorder()
	h.DisableTOTP(recDisable, reqDisable)
	assert.Equal(t, http.StatusSeeOther, recDisable.Code)
	assert.Contains(t, recDisable.Header().Get("Location"), "success=")
	assert.False(t, adminRepo.admins[adminID].TotpSecret.Valid)
}

type mockWebNodeRepo struct {
	store.NodeRepository
	nodes        map[uuid.UUID]store.Node
	deletedNodes map[uuid.UUID]bool
}

func (m *mockWebNodeRepo) GetByID(_ context.Context, id uuid.UUID) (store.Node, error) {
	if m.nodes != nil {
		if n, ok := m.nodes[id]; ok {
			return n, nil
		}
	}
	return store.Node{ID: id, Name: "mock-node"}, nil
}

func (m *mockWebNodeRepo) Delete(_ context.Context, id uuid.UUID) error {
	if m.deletedNodes == nil {
		m.deletedNodes = make(map[uuid.UUID]bool)
	}
	m.deletedNodes[id] = true
	return nil
}

type mockWebPlanRepo struct {
	store.PlanRepository
	plans map[uuid.UUID]store.Plan
}

func (m *mockWebPlanRepo) List(_ context.Context) ([]store.Plan, error) {
	var list []store.Plan
	for _, p := range m.plans {
		list = append(list, p)
	}
	return list, nil
}

func (m *mockWebPlanRepo) GetByID(_ context.Context, id uuid.UUID) (store.Plan, error) {
	if p, ok := m.plans[id]; ok {
		return p, nil
	}
	return store.Plan{}, fmt.Errorf("plan not found")
}

func (m *mockWebPlanRepo) Delete(_ context.Context, id uuid.UUID) error {
	if m.plans != nil {
		delete(m.plans, id)
	}
	return nil
}

type mockFullUserRepo struct {
	store.UserRepository
	users        map[uuid.UUID]store.User
	resetTraffic map[uuid.UUID]bool
}

func (m *mockFullUserRepo) List(_ context.Context, _ store.UserFilter) ([]store.User, int64, error) {
	var list []store.User
	for _, u := range m.users {
		list = append(list, u)
	}
	return list, int64(len(list)), nil
}

func (m *mockFullUserRepo) GetByID(_ context.Context, id uuid.UUID) (store.User, error) {
	if m.users != nil {
		if u, ok := m.users[id]; ok {
			return u, nil
		}
	}
	return store.User{ID: id, Username: "mock-user"}, nil
}

func (m *mockFullUserRepo) Create(_ context.Context, p store.CreateUserParams) (store.User, error) {
	id := uuid.New()
	u := store.User{
		ID:           id,
		Username:     p.Username,
		Email:        p.Email,
		Status:       p.Status,
		PlanID:       p.PlanID,
		TrafficLimit: p.TrafficLimit,
		ExpiresAt:    p.ExpiresAt,
		Note:         p.Note,
	}
	if m.users == nil {
		m.users = make(map[uuid.UUID]store.User)
	}
	m.users[id] = u
	return u, nil
}

func (m *mockFullUserRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.users, id)
	return nil
}

func (m *mockFullUserRepo) ResetTraffic(_ context.Context, id uuid.UUID) error {
	if m.resetTraffic == nil {
		m.resetTraffic = make(map[uuid.UUID]bool)
	}
	m.resetTraffic[id] = true
	return nil
}

func (m *mockFullUserRepo) SetBanStatus(_ context.Context, id uuid.UUID, isBanned bool, reason string) error {
	if m.users != nil {
		if u, ok := m.users[id]; ok {
			u.IsBanned = pgtype.Bool{Bool: isBanned, Valid: true}
			u.BanReason = pgtype.Text{String: reason, Valid: reason != ""}
			m.users[id] = u
		}
	}
	return nil
}

func (m *mockFullUserRepo) Update(_ context.Context, p store.UpdateUserParams) (store.User, error) {
	if m.users != nil {
		if u, ok := m.users[p.ID]; ok {
			if p.PlanID.Valid {
				u.PlanID = p.PlanID
			}
			if p.TrafficLimit.Valid {
				u.TrafficLimit = p.TrafficLimit
			}
			if p.ExpiresAt.Valid {
				u.ExpiresAt = p.ExpiresAt
			}
			if p.Status.Valid {
				u.Status = p.Status
			}
			m.users[p.ID] = u
			return u, nil
		}
	}
	return store.User{}, fmt.Errorf("user not found")
}

func TestWeb_DeleteNode(t *testing.T) {
	nodeID := uuid.New()
	nodeRepo := &mockWebNodeRepo{}
	repos := &store.Repositories{Nodes: nodeRepo}
	h := &Handler{repos: repos}

	// 1. Success as Superadmin/Owner
	r := httptest.NewRequest(http.MethodPost, "/admin/nodes/"+nodeID.String()+"/delete", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", nodeID.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r = r.WithContext(context.WithValue(r.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))

	rec := httptest.NewRecorder()
	h.DeleteNode(rec, r)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "success=Node+deleted+successfully")
	assert.True(t, nodeRepo.deletedNodes[nodeID])

	// 2. Forbidden as Regular Admin
	rAdmin := httptest.NewRequest(http.MethodPost, "/admin/nodes/"+nodeID.String()+"/delete", nil)
	rAdmin = rAdmin.WithContext(context.WithValue(rAdmin.Context(), chi.RouteCtxKey, rctx))
	rAdmin = rAdmin.WithContext(context.WithValue(rAdmin.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "admin"}))

	recAdmin := httptest.NewRecorder()
	h.DeleteNode(recAdmin, rAdmin)
	assert.Equal(t, http.StatusSeeOther, recAdmin.Code)
	assert.Contains(t, recAdmin.Header().Get("Location"), "error=Forbidden")
}

func TestWeb_CreateUser_PresetAndCustom(t *testing.T) {
	planID := uuid.New()
	planRepo := &mockWebPlanRepo{
		plans: map[uuid.UUID]store.Plan{
			planID: {
				ID:           planID,
				Name:         "Pro VPN",
				TrafficLimit: pgtype.Int8{Int64: 100 * 1024 * 1024 * 1024, Valid: true},
			},
		},
	}
	userRepo := &mockFullUserRepo{}
	repos := &store.Repositories{Users: userRepo, Plans: planRepo}
	h := &Handler{repos: repos}

	// 1. Create subscriber with Preset Plan
	formData := "username=preset_user&email=preset@vpn.test&plan_type=preset&plan_id=" + planID.String() + "&duration_days=60"
	req := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(formData))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.CreateUser(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "success=Subscriber+created+successfully")

	var createdPreset store.User
	for _, u := range userRepo.users {
		if u.Username == "preset_user" {
			createdPreset = u
			break
		}
	}
	assert.Equal(t, "preset_user", createdPreset.Username)
	assert.True(t, createdPreset.PlanID.Valid)
	assert.Equal(t, planID.String(), uuid.UUID(createdPreset.PlanID.Bytes).String())
	assert.Equal(t, int64(100*1024*1024*1024), createdPreset.TrafficLimit.Int64)
	assert.True(t, createdPreset.ExpiresAt.Valid)

	// 2. Create subscriber with Custom Quota
	customForm := "username=custom_vip&email=vip@vpn.test&plan_type=custom&traffic_limit_gb=500&duration_days=180&note=Exclusive+VIP"
	reqCustom := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(customForm))
	reqCustom.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recCustom := httptest.NewRecorder()
	h.CreateUser(recCustom, reqCustom)
	assert.Equal(t, http.StatusSeeOther, recCustom.Code)
	assert.Contains(t, recCustom.Header().Get("Location"), "success=Subscriber+created+successfully")

	var createdCustom store.User
	for _, u := range userRepo.users {
		if u.Username == "custom_vip" {
			createdCustom = u
			break
		}
	}
	assert.Equal(t, "custom_vip", createdCustom.Username)
	assert.False(t, createdCustom.PlanID.Valid)
	assert.Equal(t, int64(500*1024*1024*1024), createdCustom.TrafficLimit.Int64)
	assert.True(t, createdCustom.ExpiresAt.Valid)
	assert.Equal(t, "Exclusive VIP", createdCustom.Note.String)
}

func TestWeb_DeleteUser_And_ResetTraffic_And_Ban(t *testing.T) {
	userID := uuid.New()
	userRepo := &mockFullUserRepo{
		users: map[uuid.UUID]store.User{
			userID: {ID: userID, Username: "test_user"},
		},
	}
	repos := &store.Repositories{Users: userRepo}
	h := &Handler{repos: repos}

	// 1. Reset traffic
	reqReset := httptest.NewRequest(http.MethodPost, "/admin/users/"+userID.String()+"/reset-traffic", nil)
	rctxReset := chi.NewRouteContext()
	rctxReset.URLParams.Add("id", userID.String())
	reqReset = reqReset.WithContext(context.WithValue(reqReset.Context(), chi.RouteCtxKey, rctxReset))
	recReset := httptest.NewRecorder()
	h.ResetUserTraffic(recReset, reqReset)
	assert.Equal(t, http.StatusSeeOther, recReset.Code)
	assert.Contains(t, recReset.Header().Get("Location"), "success=Traffic+quota+reset+successfully")
	assert.True(t, userRepo.resetTraffic[userID])

	// 2. Ban/Suspend User (Safe operator action)
	reqBan := httptest.NewRequest(http.MethodPost, "/admin/users/"+userID.String()+"/ban", nil)
	reqBan = reqBan.WithContext(context.WithValue(reqBan.Context(), chi.RouteCtxKey, rctxReset))
	reqBan = reqBan.WithContext(context.WithValue(reqBan.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "admin"}))
	recBan := httptest.NewRecorder()
	h.ToggleUserBan(recBan, reqBan)
	assert.Equal(t, http.StatusSeeOther, recBan.Code)
	assert.Contains(t, recBan.Header().Get("Location"), "success=User+banned+successfully")
	assert.True(t, userRepo.users[userID].IsBanned.Bool)

	// Test Unban
	recUnban := httptest.NewRecorder()
	h.ToggleUserBan(recUnban, reqBan)
	assert.Equal(t, http.StatusSeeOther, recUnban.Code)
	assert.Contains(t, recUnban.Header().Get("Location"), "success=User+unbanned+successfully")
	assert.False(t, userRepo.users[userID].IsBanned.Bool)

	// 3. Delete user as Admin (Blocked)
	reqDelAdmin := httptest.NewRequest(http.MethodPost, "/admin/users/"+userID.String()+"/delete", nil)
	rctxDel := chi.NewRouteContext()
	rctxDel.URLParams.Add("id", userID.String())
	reqDelAdmin = reqDelAdmin.WithContext(context.WithValue(reqDelAdmin.Context(), chi.RouteCtxKey, rctxDel))
	reqDelAdmin = reqDelAdmin.WithContext(context.WithValue(reqDelAdmin.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "admin"}))
	recDelAdmin := httptest.NewRecorder()
	h.DeleteUser(recDelAdmin, reqDelAdmin)
	assert.Equal(t, http.StatusSeeOther, recDelAdmin.Code)
	assert.Contains(t, recDelAdmin.Header().Get("Location"), "error=Forbidden")
	_, stillExists := userRepo.users[userID]
	assert.True(t, stillExists)

	// 4. Delete user as Owner/Superadmin (Allowed)
	reqDelOwner := httptest.NewRequest(http.MethodPost, "/admin/users/"+userID.String()+"/delete", nil)
	reqDelOwner = reqDelOwner.WithContext(context.WithValue(reqDelOwner.Context(), chi.RouteCtxKey, rctxDel))
	reqDelOwner = reqDelOwner.WithContext(context.WithValue(reqDelOwner.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recDelOwner := httptest.NewRecorder()
	h.DeleteUser(recDelOwner, reqDelOwner)
	assert.Equal(t, http.StatusSeeOther, recDelOwner.Code)
	assert.Contains(t, recDelOwner.Header().Get("Location"), "success=User+deleted+successfully")
	_, exists := userRepo.users[userID]
	assert.False(t, exists)
}

func TestWeb_DeletePlan_RBAC(t *testing.T) {
	planID := uuid.New()
	planRepo := &mockWebPlanRepo{
		plans: map[uuid.UUID]store.Plan{
			planID: {ID: planID, Name: "Test Plan"},
		},
	}
	repos := &store.Repositories{Plans: planRepo}
	h := &Handler{repos: repos}

	// 1. Blocked as Admin
	reqAdmin := httptest.NewRequest(http.MethodPost, "/admin/plans/"+planID.String()+"/delete", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", planID.String())
	reqAdmin = reqAdmin.WithContext(context.WithValue(reqAdmin.Context(), chi.RouteCtxKey, rctx))
	reqAdmin = reqAdmin.WithContext(context.WithValue(reqAdmin.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "admin"}))
	recAdmin := httptest.NewRecorder()
	h.DeletePlan(recAdmin, reqAdmin)
	assert.Equal(t, http.StatusSeeOther, recAdmin.Code)
	assert.Contains(t, recAdmin.Header().Get("Location"), "error=Forbidden")

	// 2. Allowed as Owner
	reqOwner := httptest.NewRequest(http.MethodPost, "/admin/plans/"+planID.String()+"/delete", nil)
	reqOwner = reqOwner.WithContext(context.WithValue(reqOwner.Context(), chi.RouteCtxKey, rctx))
	reqOwner = reqOwner.WithContext(context.WithValue(reqOwner.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recOwner := httptest.NewRecorder()
	h.DeletePlan(recOwner, reqOwner)
	assert.Equal(t, http.StatusSeeOther, recOwner.Code)
	assert.Contains(t, recOwner.Header().Get("Location"), "success=Plan+deleted+successfully")
}

func TestWeb_NotFound(t *testing.T) {
	tmpl, err := NewTemplateEngine()
	require.NoError(t, err)
	h := &Handler{tmpl: tmpl}

	// 1. Browser HTML 404
	reqHTML := httptest.NewRequest(http.MethodGet, "/non-existent-page", nil)
	recHTML := httptest.NewRecorder()
	h.NotFound(recHTML, reqHTML)
	assert.Equal(t, http.StatusNotFound, recHTML.Code)
	assert.Contains(t, recHTML.Body.String(), "Page Not Found")
	assert.Contains(t, recHTML.Body.String(), "404")

	// 2. API JSON 404
	reqAPI := httptest.NewRequest(http.MethodGet, "/api/v1/non-existent-endpoint", nil)
	recAPI := httptest.NewRecorder()
	h.NotFound(recAPI, reqAPI)
	assert.Equal(t, http.StatusNotFound, recAPI.Code)
	assert.Equal(t, "application/json", recAPI.Header().Get("Content-Type"))
	assert.Contains(t, recAPI.Body.String(), "resource not found")
}

func TestWeb_CSRF_Protection(t *testing.T) {
	jwtMgr := auth.NewJWTManager("csrf-test-secret-key-at-least-32-chars-long", time.Hour, 24*time.Hour)
	adminID := uuid.New()
	secret := jwtMgr.SecretBytes()

	// 1. Token Generation and Validation
	token := GenerateCSRFToken(adminID.String(), secret, 10*time.Minute)
	assert.NotEmpty(t, token)
	assert.True(t, ValidateCSRFToken(token, adminID.String(), secret))
	assert.False(t, ValidateCSRFToken(token, uuid.New().String(), secret), "Token must fail for different admin ID")
	assert.False(t, ValidateCSRFToken(token, adminID.String(), []byte("wrong-secret")), "Token must fail for mismatched secret")

	// 2. Middleware test - Next handler stub
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	csrfMW := RequireCSRF(jwtMgr)(nextHandler)

	// GET request should pass freely without CSRF token
	getReq := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "owner"}))
	getRec := httptest.NewRecorder()
	csrfMW.ServeHTTP(getRec, getReq)
	assert.Equal(t, http.StatusOK, getRec.Code)

	// POST request without token should be blocked with 403 Forbidden
	postReqBad := httptest.NewRequest(http.MethodPost, "/admin/nodes", nil)
	postReqBad = postReqBad.WithContext(context.WithValue(postReqBad.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "owner"}))
	postRecBad := httptest.NewRecorder()
	csrfMW.ServeHTTP(postRecBad, postReqBad)
	assert.Equal(t, http.StatusForbidden, postRecBad.Code)

	// POST request with valid X-CSRF-Token header should pass
	postReqHeader := httptest.NewRequest(http.MethodPost, "/admin/nodes", nil)
	postReqHeader = postReqHeader.WithContext(context.WithValue(postReqHeader.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "owner"}))
	postReqHeader.Header.Set(CSRFHeaderName, token)
	postRecHeader := httptest.NewRecorder()
	csrfMW.ServeHTTP(postRecHeader, postReqHeader)
	assert.Equal(t, http.StatusOK, postRecHeader.Code)

	// POST request with valid form value csrf_token should pass
	formData := strings.NewReader("csrf_token=" + token + "&name=testnode")
	postReqForm := httptest.NewRequest(http.MethodPost, "/admin/nodes", formData)
	postReqForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReqForm = postReqForm.WithContext(context.WithValue(postReqForm.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "owner"}))
	postRecForm := httptest.NewRecorder()
	csrfMW.ServeHTTP(postRecForm, postReqForm)
	assert.Equal(t, http.StatusOK, postRecForm.Code)
}

type mockWebBillingRepo struct {
	store.BillingRepository
	settings store.BillingSetting
	replies  map[string]string
}

func (m *mockWebBillingRepo) GetBillingSettings(_ context.Context) (store.BillingSetting, error) {
	return m.settings, nil
}

func (m *mockWebBillingRepo) ListPaymentGateways(_ context.Context) ([]store.PaymentGateway, error) {
	return nil, nil
}

func (m *mockWebBillingRepo) GetBotReplies(_ context.Context) (map[string]string, error) {
	if m.replies == nil {
		m.replies = make(map[string]string)
	}
	return m.replies, nil
}

func (m *mockWebBillingRepo) UpsertBotReply(_ context.Context, key, text string) error {
	if m.replies == nil {
		m.replies = make(map[string]string)
	}
	m.replies[key] = text
	return nil
}

type mockWebAPIKeyRepo struct {
	store.APIKeyRepository
}

func (m *mockWebAPIKeyRepo) List(_ context.Context) ([]store.ApiKey, error) {
	return nil, nil
}

type mockTelegramSender struct {
	messages []struct {
		chatID  int64
		text    string
		buttons []service.BroadcastButton
	}
}

func (m *mockTelegramSender) SendMessage(_ context.Context, chatID int64, text string, buttons []service.BroadcastButton) error {
	m.messages = append(m.messages, struct {
		chatID  int64
		text    string
		buttons []service.BroadcastButton
	}{chatID: chatID, text: text, buttons: buttons})
	return nil
}

func TestWeb_DirectMessageUser(t *testing.T) {
	targetUserID := uuid.New()
	targetUser := store.User{
		ID:         targetUserID,
		Username:   "alex_tg",
		TelegramID: pgtype.Int8{Int64: 123456789, Valid: true},
	}
	userWithoutTg := store.User{
		ID:       uuid.New(),
		Username: "web_only_user",
	}

	userRepo := &mockFullUserRepo{
		users: map[uuid.UUID]store.User{
			targetUserID:     targetUser,
			userWithoutTg.ID: userWithoutTg,
		},
	}
	repos := &store.Repositories{Users: userRepo}
	h := &Handler{repos: repos}

	sender := &mockTelegramSender{}
	broadcastSvc := service.NewBroadcastService(nil, userRepo, sender, nil)
	h.SetBroadcastService(broadcastSvc)

	// 1. Forbidden without CanBroadcast permission (admin role has CanBroadcast=false by default)
	reqNoPerm := httptest.NewRequest(http.MethodPost, "/admin/users/"+targetUserID.String()+"/message", strings.NewReader("message_text=Hello"))
	reqNoPerm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", targetUserID.String())
	reqNoPerm = reqNoPerm.WithContext(context.WithValue(reqNoPerm.Context(), chi.RouteCtxKey, rctx))
	reqNoPerm = reqNoPerm.WithContext(context.WithValue(reqNoPerm.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "admin"}))
	recNoPerm := httptest.NewRecorder()
	h.DirectMessageUser(recNoPerm, reqNoPerm)
	assert.Equal(t, http.StatusSeeOther, recNoPerm.Code)
	assert.Contains(t, recNoPerm.Header().Get("Location"), "error=Forbidden")

	// 2. Error if message_text is empty
	reqEmpty := httptest.NewRequest(http.MethodPost, "/admin/users/"+targetUserID.String()+"/message", strings.NewReader("message_text="))
	reqEmpty.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqEmpty = reqEmpty.WithContext(context.WithValue(reqEmpty.Context(), chi.RouteCtxKey, rctx))
	reqEmpty = reqEmpty.WithContext(context.WithValue(reqEmpty.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recEmpty := httptest.NewRecorder()
	h.DirectMessageUser(recEmpty, reqEmpty)
	assert.Equal(t, http.StatusSeeOther, recEmpty.Code)
	assert.Contains(t, recEmpty.Header().Get("Location"), "error=Message+text+cannot+be+empty")

	// 3. Error if user has no Telegram account
	rctxNoTg := chi.NewRouteContext()
	rctxNoTg.URLParams.Add("id", userWithoutTg.ID.String())
	reqNoTg := httptest.NewRequest(http.MethodPost, "/admin/users/"+userWithoutTg.ID.String()+"/message", strings.NewReader("message_text=Hello"))
	reqNoTg.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqNoTg = reqNoTg.WithContext(context.WithValue(reqNoTg.Context(), chi.RouteCtxKey, rctxNoTg))
	reqNoTg = reqNoTg.WithContext(context.WithValue(reqNoTg.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recNoTg := httptest.NewRecorder()
	h.DirectMessageUser(recNoTg, reqNoTg)
	assert.Equal(t, http.StatusSeeOther, recNoTg.Code)
	assert.Contains(t, recNoTg.Header().Get("Location"), "error=User+has+no+linked+Telegram+account")

	// 4. Success sending message with button
	form := "message_text=Special+offer+for+you!&button_text=Renew+Now&button_url=https://vpn.example.com"
	reqSuccess := httptest.NewRequest(http.MethodPost, "/admin/users/"+targetUserID.String()+"/message", strings.NewReader(form))
	reqSuccess.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqSuccess = reqSuccess.WithContext(context.WithValue(reqSuccess.Context(), chi.RouteCtxKey, rctx))
	reqSuccess = reqSuccess.WithContext(context.WithValue(reqSuccess.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recSuccess := httptest.NewRecorder()
	h.DirectMessageUser(recSuccess, reqSuccess)
	assert.Equal(t, http.StatusSeeOther, recSuccess.Code)
	assert.Contains(t, recSuccess.Header().Get("Location"), "success=Personal+message+sent+to+Telegram+successfully")

	require.Len(t, sender.messages, 1)
	assert.Equal(t, int64(123456789), sender.messages[0].chatID)
	assert.Equal(t, "Special offer for you!", sender.messages[0].text)
	require.Len(t, sender.messages[0].buttons, 1)
	assert.Equal(t, "Renew Now", sender.messages[0].buttons[0].Text)
	assert.Equal(t, "https://vpn.example.com", sender.messages[0].buttons[0].URL)
}

func TestWeb_AssignUserPlan(t *testing.T) {
	planID := uuid.New()
	targetUserID := uuid.New()

	planRepo := &mockWebPlanRepo{
		plans: map[uuid.UUID]store.Plan{
			planID: {
				ID:             planID,
				Name:           "VIP Unlimited",
				TrafficLimitGb: pgtype.Int4{Int32: 200, Valid: true},
			},
		},
	}
	userRepo := &mockFullUserRepo{
		users: map[uuid.UUID]store.User{
			targetUserID: {
				ID:         targetUserID,
				Username:   "lead_user",
				TelegramID: pgtype.Int8{Int64: 555444333, Valid: true},
				Status:     pgtype.Text{String: "lead", Valid: true},
			},
		},
	}
	sender := &mockTelegramSender{}
	broadcastSvc := service.NewBroadcastService(nil, userRepo, sender, nil)

	repos := &store.Repositories{Users: userRepo, Plans: planRepo}
	h := &Handler{repos: repos}
	h.SetBroadcastService(broadcastSvc)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", targetUserID.String())

	// 1. Invalid plan ID
	reqInvalidPlan := httptest.NewRequest(http.MethodPost, "/admin/users/"+targetUserID.String()+"/assign-plan", strings.NewReader("plan_id=invalid-uuid"))
	reqInvalidPlan.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqInvalidPlan = reqInvalidPlan.WithContext(context.WithValue(reqInvalidPlan.Context(), chi.RouteCtxKey, rctx))
	reqInvalidPlan = reqInvalidPlan.WithContext(context.WithValue(reqInvalidPlan.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recInvalidPlan := httptest.NewRecorder()
	h.AssignUserPlan(recInvalidPlan, reqInvalidPlan)
	assert.Equal(t, http.StatusSeeOther, recInvalidPlan.Code)
	assert.Contains(t, recInvalidPlan.Header().Get("Location"), "error=Please+select+a+valid+plan")

	// 2. Non-existent plan
	randomPlanID := uuid.New()
	reqNonExistent := httptest.NewRequest(http.MethodPost, "/admin/users/"+targetUserID.String()+"/assign-plan", strings.NewReader("plan_id="+randomPlanID.String()))
	reqNonExistent.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqNonExistent = reqNonExistent.WithContext(context.WithValue(reqNonExistent.Context(), chi.RouteCtxKey, rctx))
	reqNonExistent = reqNonExistent.WithContext(context.WithValue(reqNonExistent.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recNonExistent := httptest.NewRecorder()
	h.AssignUserPlan(recNonExistent, reqNonExistent)
	assert.Equal(t, http.StatusSeeOther, recNonExistent.Code)
	assert.Contains(t, recNonExistent.Header().Get("Location"), "error=Plan+not+found")

	// 3. Success with Telegram notification
	form := "plan_id=" + planID.String() + "&duration_days=45&notify_user=on"
	reqSuccess := httptest.NewRequest(http.MethodPost, "/admin/users/"+targetUserID.String()+"/assign-plan", strings.NewReader(form))
	reqSuccess.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqSuccess = reqSuccess.WithContext(context.WithValue(reqSuccess.Context(), chi.RouteCtxKey, rctx))
	reqSuccess = reqSuccess.WithContext(context.WithValue(reqSuccess.Context(), AdminContextKey, &AdminContext{AdminID: uuid.New(), Role: "owner"}))
	recSuccess := httptest.NewRecorder()
	h.AssignUserPlan(recSuccess, reqSuccess)
	assert.Equal(t, http.StatusSeeOther, recSuccess.Code)
	assert.Contains(t, recSuccess.Header().Get("Location"), "success=Plan+assigned+and+provisioned+successfully")

	updatedUser := userRepo.users[targetUserID]
	assert.Equal(t, "active", updatedUser.Status.String)
	assert.Equal(t, planID.String(), uuid.UUID(updatedUser.PlanID.Bytes).String())
	assert.True(t, updatedUser.ExpiresAt.Valid)
	assert.Greater(t, updatedUser.TrafficLimit.Int64, int64(0))

	// Verify notification sent via Telegram
	require.Len(t, sender.messages, 1)
	assert.Equal(t, int64(555444333), sender.messages[0].chatID)
	assert.Contains(t, sender.messages[0].text, "VIP Unlimited")
	assert.Contains(t, sender.messages[0].text, "45 days")
}

func TestWeb_SettingsBotReplies(t *testing.T) {
	tmpl, err := NewTemplateEngine()
	require.NoError(t, err)

	billingRepo := &mockWebBillingRepo{
		replies: map[string]string{
			"welcome_new_user": "Welcome, new user!",
		},
	}
	adminRepo := &mockWebAdminRepo{
		admins: map[uuid.UUID]store.Admin{},
	}
	apiKeyRepo := &mockWebAPIKeyRepo{}

	repos := &store.Repositories{
		Billing: billingRepo,
		Admins:  adminRepo,
		APIKeys: apiKeyRepo,
	}
	h := &Handler{repos: repos, tmpl: tmpl}

	// 1. Regular admin without CanEditBotReplies permission gets redirected to dashboard
	adminID := uuid.New()
	adminRepo.admins[adminID] = store.Admin{
		ID:   adminID,
		Role: pgtype.Text{String: "admin", Valid: true},
	}
	reqAdmin := httptest.NewRequest(http.MethodGet, "/admin/settings/bot-replies", nil)
	reqAdmin = reqAdmin.WithContext(context.WithValue(reqAdmin.Context(), AdminContextKey, &AdminContext{AdminID: adminID, Role: "admin"}))
	recAdmin := httptest.NewRecorder()
	h.SettingsBotReplies(recAdmin, reqAdmin)
	assert.Equal(t, http.StatusSeeOther, recAdmin.Code)
	assert.Contains(t, recAdmin.Header().Get("Location"), "dashboard")

	// 2. Owner can view bot replies settings
	ownerID := uuid.New()
	adminRepo.admins[ownerID] = store.Admin{
		ID:   ownerID,
		Role: pgtype.Text{String: "owner", Valid: true},
	}
	reqOwner := httptest.NewRequest(http.MethodGet, "/admin/settings/bot-replies", nil)
	reqOwner = reqOwner.WithContext(context.WithValue(reqOwner.Context(), AdminContextKey, &AdminContext{AdminID: ownerID, Role: "owner"}))
	recOwner := httptest.NewRecorder()
	h.SettingsBotReplies(recOwner, reqOwner)
	assert.Equal(t, http.StatusOK, recOwner.Code)
	assert.Contains(t, recOwner.Body.String(), "Bot Replies & Texts")
	assert.Contains(t, recOwner.Body.String(), "Welcome, new user!")
	assert.Contains(t, recOwner.Body.String(), "Catalog, Checkout &amp; Invoicing")

	// 3. Owner can update bot replies
	form := "reply_welcome_new_user=Hello+and+Welcome!&reply_help_text=Contact+support+at+help&reply_referral_overview=Invite+friends+now"
	reqUpdate := httptest.NewRequest(http.MethodPost, "/admin/settings/bot-replies", strings.NewReader(form))
	reqUpdate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqUpdate = reqUpdate.WithContext(context.WithValue(reqUpdate.Context(), AdminContextKey, &AdminContext{AdminID: ownerID, Role: "owner"}))
	recUpdate := httptest.NewRecorder()
	h.UpdateBotReplies(recUpdate, reqUpdate)
	assert.Equal(t, http.StatusSeeOther, recUpdate.Code)
	assert.Contains(t, recUpdate.Header().Get("Location"), "saved=true")

	assert.Equal(t, "Hello and Welcome!", billingRepo.replies["welcome_new_user"])
	assert.Equal(t, "Contact support at help", billingRepo.replies["help_text"])
	assert.Equal(t, "Invite friends now", billingRepo.replies["referral_overview"])

	// 4. Dedicated Referral Program tab view and update
	reqRefView := httptest.NewRequest(http.MethodGet, "/admin/settings/referrals", nil)
	reqRefView = reqRefView.WithContext(context.WithValue(reqRefView.Context(), AdminContextKey, &AdminContext{AdminID: ownerID, Role: "owner"}))
	recRefView := httptest.NewRecorder()
	h.SettingsReferrals(recRefView, reqRefView)
	assert.Equal(t, http.StatusOK, recRefView.Code)
	assert.Contains(t, recRefView.Body.String(), "Referral & Growth Engine")
	assert.Contains(t, recRefView.Body.String(), "Referral Growth Program Status")

	formRef := "enabled=true&reward_model=bonus_days&inviter_days=10&invitee_days=5&qualification=first_payment&daily_cap=8&reward_expired=true"
	reqRefUpdate := httptest.NewRequest(http.MethodPost, "/admin/settings/referrals", strings.NewReader(formRef))
	reqRefUpdate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqRefUpdate = reqRefUpdate.WithContext(context.WithValue(reqRefUpdate.Context(), AdminContextKey, &AdminContext{AdminID: ownerID, Role: "owner"}))
	recRefUpdate := httptest.NewRecorder()
	h.UpdateReferralSettings(recRefUpdate, reqRefUpdate)
	assert.Equal(t, http.StatusSeeOther, recRefUpdate.Code)
	assert.Contains(t, recRefUpdate.Header().Get("Location"), "saved=true")

	assert.Equal(t, "true", billingRepo.replies["referral_enabled"])
	assert.Equal(t, "10", billingRepo.replies["referral_inviter_days"])
	assert.Equal(t, "5", billingRepo.replies["referral_invitee_days"])
	assert.Equal(t, "first_payment", billingRepo.replies["referral_qualification"])
	assert.Equal(t, "8", billingRepo.replies["referral_daily_cap"])
}

func TestWeb_TemplateEngine_UsersCRM(t *testing.T) {
	tmpl, err := NewTemplateEngine()
	require.NoError(t, err)

	now := time.Now()
	users := []store.User{
		{
			ID:               uuid.New(),
			Username:         "alice",
			TelegramUsername: pgtype.Text{String: "alice_vpn", Valid: true},
			TelegramID:       pgtype.Int8{Int64: 111222, Valid: true},
			Status:           pgtype.Text{String: "active", Valid: true},
			ExpiresAt:        pgtype.Timestamptz{Time: now.Add(30 * 24 * time.Hour), Valid: true},
		},
		{
			ID:               uuid.New(),
			Username:         "lead_bob",
			TelegramUsername: pgtype.Text{String: "bob_lead", Valid: true},
			TelegramID:       pgtype.Int8{Int64: 333444, Valid: true},
			Status:           pgtype.Text{String: "lead", Valid: true},
		},
		{
			ID:               uuid.New(),
			Username:         "banned_user",
			TelegramID:       pgtype.Int8{Int64: 999888, Valid: true},
			Status:           pgtype.Text{String: "banned", Valid: true},
			IsBanned:         pgtype.Bool{Bool: true, Valid: true},
		},
	}

	plans := []store.Plan{
		{
			ID:   uuid.New(),
			Name: "Pro Plan",
		},
	}

	rec := httptest.NewRecorder()
	err = tmpl.Render(rec, "users.html", map[string]any{
		"ActiveNav":        "users",
		"CurrentSegment":   "all",
		"TotalUsersCount":  len(users),
		"ActiveCount":      1,
		"LeadsCount":       1,
		"TrialCount":       0,
		"ExpiredCount":     0,
		"BannedCount":      1,
		"Users":            users,
		"Plans":            plans,
		"CanBroadcast":     true,
		"CanManageUsers":   true,
		"CurrentRole":      "owner",
		"Pagination":       map[string]any{"Current": 1, "Total": 1},
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Create Subscriber")
	assert.Contains(t, body, "@alice_vpn")
	assert.Contains(t, body, "@bob_lead")
	assert.Contains(t, body, "Bot Leads (1)")
	assert.Contains(t, body, "Active Subscriptions (1)")
	assert.Contains(t, body, "Send Direct Telegram Message")
	assert.Contains(t, body, "Assign Plan")
}

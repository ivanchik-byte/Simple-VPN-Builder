package web

import (
	"context"
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

func TestWeb_TemplateEngine_Standalone404(t *testing.T) {
	engine, err := NewTemplateEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)

	rec := httptest.NewRecorder()
	err = engine.RenderStandalone(rec, "error.html", map[string]any{
		"Code":    "404",
		"Title":   "Page Not Found",
		"Message": "The requested endpoint or resource was not found on this server.",
		"Accent":  "#a78bfa",
		"Glow":    "rgba(139,92,246,0.12)",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Page Not Found")
	assert.Contains(t, body, "404")
	assert.Contains(t, body, "Control Plane")
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

func (m *mockWebAdminRepo) SetMustChangePassword(_ context.Context, id uuid.UUID, must bool) error {
	if a, ok := m.admins[id]; ok {
		a.MustChangePassword = must
		m.admins[id] = a
	}
	return nil
}

func (m *mockWebAdminRepo) UpdatePassword(_ context.Context, id uuid.UUID, hash string) error {
	if a, ok := m.admins[id]; ok {
		a.PasswordHash = hash
		a.MustChangePassword = false
		m.admins[id] = a
	}
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

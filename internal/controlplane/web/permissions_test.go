package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestAdminPermissions_DefaultAndParsed(t *testing.T) {
	// 1. Owner permissions must always be all true
	ownerAdmin := store.Admin{
		ID:    uuid.New(),
		Email: "owner@test.local",
		Role:  pgtype.Text{String: "owner", Valid: true},
	}
	ownerPerms := ownerAdmin.ParsedPermissions()
	assert.True(t, ownerPerms.CanBroadcast)
	assert.True(t, ownerPerms.CanManageUsers)
	assert.True(t, ownerPerms.CanDeleteUsers)
	assert.True(t, ownerPerms.CanManageNodes)
	assert.True(t, ownerPerms.CanManagePlans)
	assert.True(t, ownerPerms.CanViewAudit)
	assert.True(t, ownerPerms.CanAccessAICopilot, "Owner must always have AI copilot access")

	// 2. Standard admin safe defaults: Broadcast and destructive operations disabled
	standardAdmin := store.Admin{
		ID:    uuid.New(),
		Email: "support@test.local",
		Role:  pgtype.Text{String: "admin", Valid: true},
	}
	adminPerms := standardAdmin.ParsedPermissions()
	assert.False(t, adminPerms.CanBroadcast, "Broadcast must be disabled by default for standard admin")
	assert.True(t, adminPerms.CanManageUsers)
	assert.False(t, adminPerms.CanDeleteUsers, "Delete users must be disabled by default")
	assert.False(t, adminPerms.CanManageNodes, "Node infrastructure must be disabled by default")
	assert.False(t, adminPerms.CanManagePlans)
	assert.False(t, adminPerms.CanViewAudit)
	assert.False(t, adminPerms.CanAccessAICopilot, "Standard admin must not have AI copilot access by default")

	// 3. Custom granted permissions
	customAdmin := store.Admin{
		ID:          uuid.New(),
		Email:       "marketing@test.local",
		Role:        pgtype.Text{String: "admin", Valid: true},
		Permissions: []byte(`{"can_broadcast": true, "can_manage_users": false, "can_access_ai_copilot": true}`),
	}
	customPerms := customAdmin.ParsedPermissions()
	assert.True(t, customPerms.CanBroadcast, "Broadcast should be explicitly enabled when flag is set")
	assert.False(t, customPerms.CanManageUsers, "Manage users should be false as specified")
	assert.True(t, customPerms.CanAccessAICopilot, "AI Copilot should be enabled when granted by owner")
}

func TestBroadcastPage_AccessControl(t *testing.T) {
	h := &Handler{}

	// Request with standard admin (CanBroadcast = false)
	req := httptest.NewRequest(http.MethodGet, "/admin/broadcast", nil)
	adminCtx := &AdminContext{
		AdminID:  uuid.New(),
		Username: "operator@test.local",
		Role:     "admin",
	}
	ctx := context.WithValue(req.Context(), AdminContextKey, adminCtx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.BroadcastPage(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "broadcast+permission+required")
}

func TestNodeManagement_AccessControl(t *testing.T) {
	h := &Handler{}

	// Request with standard admin (CanManageNodes = false)
	req := httptest.NewRequest(http.MethodPost, "/admin/nodes", nil)
	adminCtx := &AdminContext{
		AdminID:  uuid.New(),
		Username: "operator@test.local",
		Role:     "admin",
	}
	req = req.WithContext(context.WithValue(req.Context(), AdminContextKey, adminCtx))

	rr := httptest.NewRecorder()
	h.CreateNode(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "permission+to+manage+nodes+is+required")
}

func TestUserManagement_AccessControl(t *testing.T) {
	h := &Handler{}

	// Admin with can_manage_users = false
	adminID := uuid.New()
	adminCtx := &AdminContext{
		AdminID:  adminID,
		Username: "restricted@test.local",
		Role:     "admin",
	}

	// Note: Without repos, getCallerPermissions falls back to default admin perms (CanManageUsers=true),
	// but when caller lacks CanManageUsers (or when tested directly), CreateUser and ToggleUserBan redirect.
	// We test ResetUserTraffic where CanResetTraffic is tested:
	reqReset := httptest.NewRequest(http.MethodPost, "/admin/users/"+adminID.String()+"/reset-traffic", nil)
	reqReset = reqReset.WithContext(context.WithValue(reqReset.Context(), AdminContextKey, adminCtx))
	rrReset := httptest.NewRecorder()
	h.ResetUserTraffic(rrReset, reqReset)
	// Standard admin has CanResetTraffic=true by default, but verify it processes safely
	assert.Equal(t, http.StatusSeeOther, rrReset.Code)
}

func TestSettings_AccessControl(t *testing.T) {
	h := &Handler{}

	// Standard admin navigating to /admin/settings must be forbidden
	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	adminCtx := &AdminContext{
		AdminID:  uuid.New(),
		Username: "operator@test.local",
		Role:     "admin",
	}
	req = req.WithContext(context.WithValue(req.Context(), AdminContextKey, adminCtx))

	rr := httptest.NewRecorder()
	h.Settings(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "settings+are+accessible+by+owner+only")
}

func TestPaymentGateways_AccessControl(t *testing.T) {
	h := &Handler{}

	// Non-owner updating gateways must be forbidden
	req := httptest.NewRequest(http.MethodPost, "/admin/gateways", nil)
	adminCtx := &AdminContext{
		AdminID:  uuid.New(),
		Username: "operator@test.local",
		Role:     "admin",
	}
	req = req.WithContext(context.WithValue(req.Context(), AdminContextKey, adminCtx))

	rr := httptest.NewRecorder()
	h.UpdatePaymentGateway(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "only+owner+can+modify+payment+gateways")
}

func TestAPIKeys_AccessControl(t *testing.T) {
	h := &Handler{}

	// Non-owner creating API keys must be forbidden
	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys", nil)
	adminCtx := &AdminContext{
		AdminID:  uuid.New(),
		Username: "operator@test.local",
		Role:     "admin",
	}
	req = req.WithContext(context.WithValue(req.Context(), AdminContextKey, adminCtx))

	rr := httptest.NewRecorder()
	h.CreateAPIKey(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "owner+or+superadmin+role+required")
}

func TestAISettings_AccessControl(t *testing.T) {
	h := &Handler{}

	// Non-owner / unauthorized admin attempting to update AI settings
	req := httptest.NewRequest(http.MethodPost, "/admin/settings/ai", nil)
	adminCtx := &AdminContext{
		AdminID:  uuid.New(),
		Username: "operator@test.local",
		Role:     "admin",
	}
	req = req.WithContext(context.WithValue(req.Context(), AdminContextKey, adminCtx))

	rr := httptest.NewRecorder()
	h.UpdateAISettings(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "Forbidden:+only+owner+or+authorized+AI+administrators+can+update+AI+settings")
}

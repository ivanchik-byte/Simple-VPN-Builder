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

	// 3. Custom granted permissions
	customAdmin := store.Admin{
		ID:          uuid.New(),
		Email:       "marketing@test.local",
		Role:        pgtype.Text{String: "admin", Valid: true},
		Permissions: []byte(`{"can_broadcast": true, "can_manage_users": false}`),
	}
	customPerms := customAdmin.ParsedPermissions()
	assert.True(t, customPerms.CanBroadcast, "Broadcast should be explicitly enabled when flag is set")
	assert.False(t, customPerms.CanManageUsers, "Manage users should be false as specified")
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

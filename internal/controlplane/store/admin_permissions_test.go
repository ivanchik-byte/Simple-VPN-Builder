package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputePermissionDiff(t *testing.T) {
	oldP := AdminPermissions{
		CanBroadcast:    false,
		CanManageUsers:  true,
		CanDeleteUsers:  false,
		CanResetTraffic: true,
		CanManageNodes:  false,
		CanManagePlans:  false,
		CanViewAudit:    false,
	}

	newP := AdminPermissions{
		CanBroadcast:    true,
		CanManageUsers:  true,
		CanDeleteUsers:  false,
		CanResetTraffic: true,
		CanManageNodes:  true,
		CanManagePlans:  false,
		CanViewAudit:    false,
	}

	diff := ComputePermissionDiff(oldP, newP)
	assert.Len(t, diff, 2)

	fields := make(map[string]PermissionChange)
	for _, c := range diff {
		fields[c.Field] = c
	}

	assert.Contains(t, fields, "can_broadcast")
	assert.True(t, fields["can_broadcast"].Granted)
	assert.False(t, fields["can_broadcast"].OldValue)
	assert.True(t, fields["can_broadcast"].NewValue)
	assert.Equal(t, "high", fields["can_broadcast"].Severity)

	assert.Contains(t, fields, "can_manage_nodes")
	assert.True(t, fields["can_manage_nodes"].Granted)
	assert.False(t, fields["can_manage_nodes"].OldValue)
	assert.True(t, fields["can_manage_nodes"].NewValue)
	assert.Equal(t, "high", fields["can_manage_nodes"].Severity)

	// Revoke scenario
	revokedP := AdminPermissions{
		CanBroadcast:    false,
		CanManageUsers:  false,
		CanDeleteUsers:  false,
		CanResetTraffic: true,
		CanManageNodes:  false,
		CanManagePlans:  false,
		CanViewAudit:    false,
	}
	diffRevoke := ComputePermissionDiff(newP, revokedP)
	assert.Len(t, diffRevoke, 3) // can_broadcast, can_manage_users, can_manage_nodes revoked

	for _, c := range diffRevoke {
		assert.False(t, c.Granted)
	}

	// Test AI copilot permission change
	aiGranted := newP
	aiGranted.CanAccessAICopilot = true
	diffAI := ComputePermissionDiff(newP, aiGranted)
	assert.Len(t, diffAI, 1)
	assert.Equal(t, "can_access_ai_copilot", diffAI[0].Field)
	assert.True(t, diffAI[0].Granted)
	assert.Equal(t, "high", diffAI[0].Severity)
}

func TestAdminPermissions_RoleDefaultsAndOwnerOverride(t *testing.T) {
	// Owner must have all permissions true, including CanAccessAICopilot
	ownerDefaults := DefaultAdminPermissions("owner")
	assert.True(t, ownerDefaults.CanAccessAICopilot)
	assert.True(t, ownerDefaults.CanBroadcast)
	assert.True(t, ownerDefaults.CanManageUsers)
	assert.True(t, ownerDefaults.CanManageNodes)

	// Superadmin must NOT have CanAccessAICopilot by default (owner-only by default)
	superDefaults := DefaultAdminPermissions("superadmin")
	assert.False(t, superDefaults.CanAccessAICopilot, "Superadmin must not have AI copilot access by default")
	assert.True(t, superDefaults.CanManageUsers)
	assert.True(t, superDefaults.CanManageNodes)

	// Regular admin must not have AI access
	adminDefaults := DefaultAdminPermissions("admin")
	assert.False(t, adminDefaults.CanAccessAICopilot)
}

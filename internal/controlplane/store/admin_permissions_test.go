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
}

package store

import (
	"encoding/json"
)

// AdminPermissions defines granular permission flags for system administrators.
type AdminPermissions struct {
	CanBroadcast    bool `json:"can_broadcast"`
	CanManageUsers  bool `json:"can_manage_users"`
	CanDeleteUsers  bool `json:"can_delete_users"`
	CanResetTraffic bool `json:"can_reset_traffic"`
	CanManageNodes  bool `json:"can_manage_nodes"`
	CanManagePlans  bool `json:"can_manage_plans"`
	CanViewAudit    bool `json:"can_view_audit"`
}

// DefaultAdminPermissions returns default safe permission flags for a given role.
func DefaultAdminPermissions(role string) AdminPermissions {
	if role == "owner" || role == "superadmin" {
		return AdminPermissions{
			CanBroadcast:    true,
			CanManageUsers:  true,
			CanDeleteUsers:  true,
			CanResetTraffic: true,
			CanManageNodes:  true,
			CanManagePlans:  true,
			CanViewAudit:    true,
		}
	}
	// Safe default for standard admin
	return AdminPermissions{
		CanBroadcast:    false,
		CanManageUsers:  true,
		CanDeleteUsers:  false,
		CanResetTraffic: true,
		CanManageNodes:  false,
		CanManagePlans:  false,
		CanViewAudit:    false,
	}
}

// ParsedPermissions returns typed permission flags for an administrator account.
func (a Admin) ParsedPermissions() AdminPermissions {
	p := DefaultAdminPermissions(a.Role.String)
	if len(a.Permissions) > 0 {
		_ = json.Unmarshal(a.Permissions, &p)
	}
	if a.Role.String == "owner" {
		// Owner always retains all permissions
		p.CanBroadcast = true
		p.CanManageUsers = true
		p.CanDeleteUsers = true
		p.CanResetTraffic = true
		p.CanManageNodes = true
		p.CanManagePlans = true
		p.CanViewAudit = true
	}
	return p
}

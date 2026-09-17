package store

import (
	"encoding/json"
)

// AdminPermissions defines granular permission flags for system administrators.
type AdminPermissions struct {
	CanBroadcast      bool `json:"can_broadcast"`
	CanManageUsers    bool `json:"can_manage_users"`
	CanDeleteUsers    bool `json:"can_delete_users"`
	CanResetTraffic   bool `json:"can_reset_traffic"`
	CanManageNodes    bool `json:"can_manage_nodes"`
	CanManagePlans    bool `json:"can_manage_plans"`
	CanViewAudit      bool `json:"can_view_audit"`
	CanEditBotReplies  bool `json:"can_edit_bot_replies"`
	CanManagePartners  bool `json:"can_manage_partners"`
	CanAccessAICopilot bool `json:"can_access_ai_copilot"`
	CanViewAPIKeys    bool `json:"can_view_api_keys"`
	CanManageBilling  bool `json:"can_manage_billing"`
}

// DefaultAdminPermissions returns default safe permission flags for a given role.
func DefaultAdminPermissions(role string) AdminPermissions {
	if role == "owner" {
		return AdminPermissions{
			CanBroadcast:       true,
			CanManageUsers:     true,
			CanDeleteUsers:     true,
			CanResetTraffic:    true,
			CanManageNodes:     true,
			CanManagePlans:     true,
		CanViewAudit:       true,
		CanEditBotReplies:  true,
		CanManagePartners:  true,
		CanAccessAICopilot: true,
		CanViewAPIKeys:    true,
		CanManageBilling:  true,
		}
	}
	if role == "superadmin" {
		return AdminPermissions{
			CanBroadcast:       true,
			CanManageUsers:     true,
			CanDeleteUsers:     true,
			CanResetTraffic:    true,
			CanManageNodes:     true,
			CanManagePlans:     true,
			CanViewAudit:       true,
			CanEditBotReplies:  true,
			CanManagePartners:  true,
			CanAccessAICopilot: false, // Default false unless granted by owner
			CanViewAPIKeys:    true,
			CanManageBilling:  false,
		}
	}
	// Safe default for standard admin
	return AdminPermissions{
		CanBroadcast:       false,
		CanManageUsers:     true,
		CanDeleteUsers:     false,
		CanResetTraffic:    true,
		CanManageNodes:     false,
		CanManagePlans:     false,
		CanViewAudit:       false,
		CanEditBotReplies:  false,
		CanManagePartners:  false,
		CanAccessAICopilot: false,
		CanViewAPIKeys:    false,
		CanManageBilling:  false,
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
		p.CanEditBotReplies = true
		p.CanManagePartners = true
		p.CanAccessAICopilot = true
		p.CanViewAPIKeys = true
		p.CanManageBilling = true
	}
	return p
}

// PermissionChange represents a single permission toggle delta.
type PermissionChange struct {
	Field       string `json:"field"`
	OldValue    bool   `json:"old_value"`
	NewValue    bool   `json:"new_value"`
	Granted     bool   `json:"granted"`
	Severity    string `json:"severity"` // "high", "medium", "low"
	Description string `json:"description"`
}

// AuditRBACDiffPayload represents structured JSON payload stored in audit_logs.diff.
type AuditRBACDiffPayload struct {
	TargetAdminID    string             `json:"target_admin_id"`
	TargetAdminEmail string             `json:"target_admin_email"`
	TargetRole       string             `json:"target_role"`
	Status           string             `json:"status"` // "SUCCESS", "DENIED", "SUSPICIOUS"
	Summary          string             `json:"summary"`
	Changes          []PermissionChange `json:"changes,omitempty"`
}

// ComputePermissionDiff compares two AdminPermissions and returns the list of changes.
func ComputePermissionDiff(oldP, newP AdminPermissions) []PermissionChange {
	var changes []PermissionChange

	check := func(field string, oldVal, newVal bool, severity, desc string) {
		if oldVal != newVal {
			changes = append(changes, PermissionChange{
				Field:       field,
				OldValue:    oldVal,
				NewValue:    newVal,
				Granted:     newVal,
				Severity:    severity,
				Description: desc,
			})
		}
	}

	check("can_broadcast", oldP.CanBroadcast, newP.CanBroadcast, "high", "Telegram Broadcast channel messaging")
	check("can_manage_users", oldP.CanManageUsers, newP.CanManageUsers, "medium", "Create, edit, and view subscribers")
	check("can_delete_users", oldP.CanDeleteUsers, newP.CanDeleteUsers, "high", "Permanently delete subscribers")
	check("can_reset_traffic", oldP.CanResetTraffic, newP.CanResetTraffic, "low", "Reset data traffic consumption counters")
	check("can_manage_nodes", oldP.CanManageNodes, newP.CanManageNodes, "high", "Register, edit, or delete VPN cluster nodes")
	check("can_manage_plans", oldP.CanManagePlans, newP.CanManagePlans, "medium", "Create and modify subscription plans & pricing")
	check("can_view_audit", oldP.CanViewAudit, newP.CanViewAudit, "low", "Inspect administrative audit trail and security logs")
	check("can_edit_bot_replies", oldP.CanEditBotReplies, newP.CanEditBotReplies, "medium", "Configure bot replies and custom text messages")
	check("can_manage_partners", oldP.CanManagePartners, newP.CanManagePartners, "high", "Manage white-label partners and reseller bots")
	check("can_access_ai_copilot", oldP.CanAccessAICopilot, newP.CanAccessAICopilot, "high", "Access AI Infrastructure Copilot for operations and diagnostics")
	check("can_view_api_keys", oldP.CanViewAPIKeys, newP.CanViewAPIKeys, "high", "View and manage developer API keys")
	check("can_manage_billing", oldP.CanManageBilling, newP.CanManageBilling, "high", "Configure billing, payment gateways and tariffs")

	return changes
}

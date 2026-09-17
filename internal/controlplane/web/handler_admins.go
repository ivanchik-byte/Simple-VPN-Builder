package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/alerting"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgtype"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

func (h *Handler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	callerRole := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}

	if callerRole != "owner" && callerRole != "superadmin" {
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+owner+or+superadmin+role+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings?error=Invalid+form+submission", http.StatusSeeOther)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	role := strings.TrimSpace(r.FormValue("role"))
	if role == "" {
		role = "admin"
	}

	// Superadmins can only create regular admins
	if callerRole == "superadmin" && (role == "owner" || role == "superadmin") {
		http.Redirect(w, r, "/admin/settings?error=Superadmins+can+only+create+regular+admin+accounts", http.StatusSeeOther)
		return
	}

	// Validate role
	if role != "owner" && role != "superadmin" && role != "admin" {
		http.Redirect(w, r, "/admin/settings?error=Invalid+role+specified", http.StatusSeeOther)
		return
	}

	if email == "" || password == "" {
		http.Redirect(w, r, "/admin/settings?error=Email+and+password+are+required", http.StatusSeeOther)
		return
	}

	if len(password) < 8 {
		http.Redirect(w, r, "/admin/settings?error=Password+must+be+at+least+8+characters", http.StatusSeeOther)
		return
	}

	hash, err := h.passwordManager.Hash(password)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+hash+password", http.StatusSeeOther)
		return
	}

	newAdmin, err := h.repos.Admins.Create(r.Context(), store.CreateAdminParams{
		Email:        email,
		PasswordHash: hash,
		Role:         pgtype.Text{String: role, Valid: true},
	})
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+create+admin:+email+may+already+exist", http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "CreateAdmin", "admin", &newAdmin.ID, fmt.Sprintf("Admin %s created with role %s", email, role))

	http.Redirect(w, r, "/admin/settings?success=Administrator+created+successfully", http.StatusSeeOther)
}

// errSoleOwner signals an attempt to delete the last remaining Owner account.
var errSoleOwner = errors.New("cannot delete the sole remaining owner")

// deleteOwnerGuarded recounts owners and deletes the target atomically under
// the "admin-delete" advisory lock, closing the check-then-delete TOCTOU
// window across CP instances. Falls back to the plain sequence when no
// Transactor is configured (e.g. unit tests with mock repositories).
func (h *Handler) deleteOwnerGuarded(r *http.Request, adminID uuid.UUID) error {
	remove := func() error {
		admins, err := h.repos.Admins.List(r.Context())
		if err != nil {
			return err
		}
		ownerCount := 0
		for _, a := range admins {
			if a.Role.Valid && a.Role.String == "owner" {
				ownerCount++
			}
		}
		if ownerCount <= 1 {
			return errSoleOwner
		}
		return h.repos.Admins.Delete(r.Context(), adminID)
	}
	if h.repos != nil && h.repos.Tx != nil {
		return h.repos.Tx.WithAdvisoryLock(r.Context(), "admin-delete", func(_ *store.Queries) error {
			return remove()
		})
	}
	return remove()
}

// POST /admin/admins/{id}/delete

func (h *Handler) DeleteAdmin(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	callerRole := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}

	if callerRole != "owner" && callerRole != "superadmin" {
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+insufficient+privileges", http.StatusSeeOther)
		return
	}

	adminIDStr := chi.URLParam(r, "id")
	adminID, err := uuid.Parse(adminIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Invalid+administrator+ID", http.StatusSeeOther)
		return
	}

	// Rule 1: Prevent deleting yourself
	if adminCtx.AdminID == adminID {
		http.Redirect(w, r, "/admin/settings?error=You+cannot+delete+your+own+account", http.StatusSeeOther)
		return
	}

	// Fetch target admin to check role
	targetAdmin, err := h.repos.Admins.GetByID(r.Context(), adminID)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Administrator+not+found", http.StatusSeeOther)
		return
	}

	targetRole := "admin"
	if targetAdmin.Role.Valid && targetAdmin.Role.String != "" {
		targetRole = targetAdmin.Role.String
	}

	// Rule 2: Owner deletion rules
	if targetRole == "owner" {
		if callerRole != "owner" {
			http.Redirect(w, r, "/admin/settings?error=Forbidden:+only+an+Owner+can+delete+another+Owner+account", http.StatusSeeOther)
			return
		}
		if err := h.deleteOwnerGuarded(r, adminID); err != nil {
			if errors.Is(err, errSoleOwner) {
				http.Redirect(w, r, "/admin/settings?error=Cannot+delete+the+sole+remaining+Owner.+Create+another+Owner+first+before+deleting+this+one.", http.StatusSeeOther)
			} else {
				http.Redirect(w, r, "/admin/settings?error=Failed+to+delete+administrator", http.StatusSeeOther)
			}
			return
		}
		h.recordAudit(r, "DeleteAdmin", "admin", &adminID, fmt.Sprintf("Admin %s (%s) deleted", targetAdmin.Email, targetRole))
		http.Redirect(w, r, "/admin/settings?success=Administrator+deleted+successfully", http.StatusSeeOther)
		return
	}

	// Rule 3: Only Owner can delete Superadmin
	if targetRole == "superadmin" && callerRole != "owner" {
		http.Redirect(w, r, "/admin/settings?error=Superadmins+can+only+be+deleted+by+the+Owner", http.StatusSeeOther)
		return
	}

	// Superadmin or Owner can delete Admin, or Owner can delete another Owner
	if err := h.repos.Admins.Delete(r.Context(), adminID); err != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+delete+administrator", http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "DeleteAdmin", "admin", &adminID, fmt.Sprintf("Admin %s (%s) deleted", targetAdmin.Email, targetRole))

	http.Redirect(w, r, "/admin/settings?success=Administrator+deleted+successfully", http.StatusSeeOther)
}

// POST /admin/admins/{id}/permissions

func (h *Handler) UpdateAdminPermissions(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	callerRole := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}

	ipStr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ipStr = r.RemoteAddr
	}

	adminIDStr := chi.URLParam(r, "id")
	adminID, parseErr := uuid.Parse(adminIDStr)

	if callerRole != "owner" {
		// Log privilege escalation attempt to audit logs and dispatch immediate Telegram alert
		escalationReason := fmt.Sprintf("Non-owner account %s (role: %s) attempted to modify permissions for admin %s", adminCtx.Username, callerRole, adminIDStr)
		payload := store.AuditRBACDiffPayload{
			TargetAdminID: adminIDStr,
			Status:        "DENIED",
			Summary:       escalationReason,
		}
		diffJSON, _ := json.Marshal(payload)
		var resUUID pgtype.UUID
		if parseErr == nil {
			resUUID = pgtype.UUID{Bytes: adminID, Valid: true}
		}
		var parsedIP *netip.Addr
		if addr, err := netip.ParseAddr(ipStr); err == nil {
			parsedIP = &addr
		}
		if h.repos != nil && h.repos.AuditLogs != nil {
			_, _ = h.repos.AuditLogs.Create(r.Context(), store.CreateAuditLogParams{
				AdminID:      pgtype.UUID{Bytes: adminCtx.AdminID, Valid: true},
				ApiKeyID:     pgtype.UUID{Valid: false},
				Action:       "PrivilegeEscalationAttempt",
				ResourceType: pgtype.Text{String: "admin", Valid: true},
				ResourceID:   resUUID,
				Diff:         diffJSON,
				IpAddress:    parsedIP,
				UserAgent:    pgtype.Text{String: r.UserAgent(), Valid: r.UserAgent() != ""},
			})
		}
		if h.alertDispatcher != nil {
			h.alertDispatcher.SendRBACAlert(adminCtx.Username, callerRole, adminIDStr, "unknown", ipStr, "DENIED", nil, escalationReason)
		}
		http.Redirect(w, r, "/admin/settings?error=Access+denied:+Only+Owner+can+modify+administrator+permissions", http.StatusSeeOther)
		return
	}

	if parseErr != nil {
		http.Redirect(w, r, "/admin/settings?error=Invalid+admin+ID", http.StatusSeeOther)
		return
	}

	targetAdmin, err := h.repos.Admins.GetByID(r.Context(), adminID)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Admin+not+found", http.StatusSeeOther)
		return
	}

	targetRole := "admin"
	if targetAdmin.Role.Valid && targetAdmin.Role.String != "" {
		targetRole = targetAdmin.Role.String
	}

	if targetRole == "owner" {
		http.Redirect(w, r, "/admin/settings?error=Owner+permissions+are+unrestricted+and+cannot+be+modified", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	oldPerms := targetAdmin.ParsedPermissions()

	newPerms := store.AdminPermissions{
		CanBroadcast:       r.FormValue("can_broadcast") == "on" || r.FormValue("can_broadcast") == "true",
		CanManageUsers:     r.FormValue("can_manage_users") == "on" || r.FormValue("can_manage_users") == "true",
		CanDeleteUsers:     r.FormValue("can_delete_users") == "on" || r.FormValue("can_delete_users") == "true",
		CanResetTraffic:    r.FormValue("can_reset_traffic") == "on" || r.FormValue("can_reset_traffic") == "true",
		CanManageNodes:     r.FormValue("can_manage_nodes") == "on" || r.FormValue("can_manage_nodes") == "true",
		CanManagePlans:     r.FormValue("can_manage_plans") == "on" || r.FormValue("can_manage_plans") == "true",
		CanViewAudit:       r.FormValue("can_view_audit") == "on" || r.FormValue("can_view_audit") == "true",
		CanEditBotReplies:  r.FormValue("can_edit_bot_replies") == "on" || r.FormValue("can_edit_bot_replies") == "true",
		CanManagePartners:  r.FormValue("can_manage_partners") == "on" || r.FormValue("can_manage_partners") == "true",
		CanAccessAICopilot: r.FormValue("can_access_ai_copilot") == "on" || r.FormValue("can_access_ai_copilot") == "true",
		CanViewAPIKeys:     r.FormValue("can_view_api_keys") == "on" || r.FormValue("can_view_api_keys") == "true",
		CanManageBilling:   r.FormValue("can_manage_billing") == "on" || r.FormValue("can_manage_billing") == "true",
	}

	diffChanges := store.ComputePermissionDiff(oldPerms, newPerms)

	permBytes, err := json.Marshal(newPerms)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+serialize+permissions", http.StatusSeeOther)
		return
	}

	if err := h.repos.Admins.UpdatePermissions(r.Context(), adminID, permBytes); err != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+update+permissions", http.StatusSeeOther)
		return
	}

	summary := fmt.Sprintf("Permissions updated for %s (%d changes)", targetAdmin.Email, len(diffChanges))
	auditPayload := store.AuditRBACDiffPayload{
		TargetAdminID:    adminID.String(),
		TargetAdminEmail: targetAdmin.Email,
		TargetRole:       targetRole,
		Status:           "SUCCESS",
		Summary:          summary,
		Changes:          diffChanges,
	}
	diffJSON, _ := json.Marshal(auditPayload)

	var parsedIP *netip.Addr
	if addr, err := netip.ParseAddr(ipStr); err == nil {
		parsedIP = &addr
	}

	if h.repos != nil && h.repos.AuditLogs != nil {
		_, _ = h.repos.AuditLogs.Create(r.Context(), store.CreateAuditLogParams{
			AdminID:      pgtype.UUID{Bytes: adminCtx.AdminID, Valid: true},
			ApiKeyID:     pgtype.UUID{Valid: false},
			Action:       "UpdateAdminPermissions",
			ResourceType: pgtype.Text{String: "admin", Valid: true},
			ResourceID:   pgtype.UUID{Bytes: adminID, Valid: true},
			Diff:         diffJSON,
			IpAddress:    parsedIP,
			UserAgent:    pgtype.Text{String: r.UserAgent(), Valid: r.UserAgent() != ""},
		})
	}

	if h.alertDispatcher != nil {
		var alertItems []alerting.RBACDiffItem
		for _, ch := range diffChanges {
			alertItems = append(alertItems, alerting.RBACDiffItem{
				Field:       ch.Field,
				OldValue:    ch.OldValue,
				NewValue:    ch.NewValue,
				Granted:     ch.Granted,
				Severity:    ch.Severity,
				Description: ch.Description,
			})
		}
		h.alertDispatcher.SendRBACAlert(adminCtx.Username, callerRole, targetAdmin.Email, targetRole, ipStr, "SUCCESS", alertItems, summary)
	}

	http.Redirect(w, r, "/admin/settings?success=Administrator+permissions+updated+successfully", http.StatusSeeOther)
}

// POST /admin/2fa/enable

func (h *Handler) EnableTOTP(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	secret := strings.TrimSpace(r.FormValue("secret"))
	code := strings.TrimSpace(r.FormValue("code"))
	if secret == "" || code == "" {
		http.Redirect(w, r, "/admin/settings?error=Secret+key+and+verification+code+are+required", http.StatusSeeOther)
		return
	}

	if !h.totpManager.ValidateCode(code, secret) {
		http.Redirect(w, r, "/admin/settings?error=Invalid+2FA+passcode.+Please+check+the+code+in+your+authenticator+app+and+try+again", http.StatusSeeOther)
		return
	}

	admin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Admin+account+not+found", http.StatusSeeOther)
		return
	}

	_, err = h.repos.Admins.Update(r.Context(), store.UpdateAdminParams{
		ID:           admin.ID,
		Email:        admin.Email,
		PasswordHash: admin.PasswordHash,
		Role:         admin.Role,
		TotpSecret:   pgtype.Text{String: secret, Valid: true},
	})
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+activate+2FA", http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "Enable2FA", "admin", &admin.ID, fmt.Sprintf("Two-factor authentication (TOTP) activated for account %s", admin.Email))
	http.Redirect(w, r, "/admin/settings?success=Two-factor+authentication+(2FA)+has+been+successfully+enabled!", http.StatusSeeOther)
}

// POST /admin/2fa/disable

func (h *Handler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	admin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID)
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Admin+account+not+found", http.StatusSeeOther)
		return
	}

	_, err = h.repos.Admins.Update(r.Context(), store.UpdateAdminParams{
		ID:           admin.ID,
		Email:        admin.Email,
		PasswordHash: admin.PasswordHash,
		Role:         admin.Role,
		TotpSecret:   pgtype.Text{Valid: false},
	})
	if err != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+disable+2FA", http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "Disable2FA", "admin", &admin.ID, fmt.Sprintf("Two-factor authentication (TOTP) disabled for account %s", admin.Email))
	http.Redirect(w, r, "/admin/settings?success=Two-factor+authentication+(2FA)+has+been+disabled.", http.StatusSeeOther)
}

// POST /admin/api-keys

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	callerRole := adminCtx.Role
	if h.repos != nil && h.repos.Admins != nil {
		if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
			callerRole = callerAdmin.Role.String
		}
	}
	if callerRole != "owner" && callerRole != "superadmin" && !h.getCallerPermissions(r.Context()).CanViewAPIKeys {
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+owner+or+superadmin+role+required+to+create+API+keys", http.StatusSeeOther)
		return
	}

	rawKey, keyHash, err := h.apiKeyManager.GenerateKey()
	if err != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	prefix := rawKey[:12]
	keyName := strings.TrimSpace(r.FormValue("name"))
	if keyName == "" {
		keyName = "API Key (" + prefix + ")"
	}
	// Multi-scope issuance: accept repeated "scope"/"scopes" fields plus
	// comma-separated values; fall back to the legacy single "scope" field.
	scopes := parseKeyScopes(r)
	if callerRole != "owner" {
		if msg := checkKeyScopeGrant(h.getCallerPermissions(r.Context()), scopes); msg != "" {
			http.Redirect(w, r, "/admin/settings?error="+url.QueryEscape(msg), http.StatusSeeOther)
			return
		}
	}
	created, createErr := h.repos.APIKeys.Create(r.Context(), store.CreateAPIKeyParams{
		Name:      keyName,
		Prefix:    prefix,
		KeyHash:   keyHash,
		Scopes:    scopes,
		CreatedBy: pgtype.UUID{Bytes: adminCtx.AdminID, Valid: true},
	})
	if createErr != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+create+API+key", http.StatusSeeOther)
		return
	}

	logger.InfoContext(r.Context(), "API key created", "prefix", prefix, "id", created.ID)
	h.recordAudit(r, "CreateAPIKey", "api_key", &created.ID, fmt.Sprintf("Issued API key '%s' (prefix: %s, scopes: %s)", created.Name, prefix, strings.Join(scopes, ",")))
	http.Redirect(w, r, "/admin/settings?generated_key="+url.QueryEscape(rawKey)+"&success=API+key+issued+successfully", http.StatusSeeOther)
}

// parseKeyScopes collects API key scopes from repeated "scope"/"scopes" form
// fields and comma-separated values, defaulting to full admin access.
// Unknown scopes are dropped so forged form values cannot mint wildcards.
func parseKeyScopes(r *http.Request) []string {
	allowed := map[string]bool{
		"admin": true, "*": true,
		"user:read": true, "user:write": true,
		"node:read": true, "node:write": true,
		"billing:read": true, "billing:write": true,
		"ai": true,
	}
	seen := map[string]bool{}
	var scopes []string
	add := func(v string) {
		for _, part := range strings.Split(v, ",") {
			s := strings.ToLower(strings.TrimSpace(part))
			if s == "" || seen[s] || !allowed[s] {
				continue
			}
			seen[s] = true
			scopes = append(scopes, s)
		}
	}
	for _, v := range r.Form["scope"] {
		add(v)
	}
	for _, v := range r.Form["scopes"] {
		add(v)
	}
	if len(scopes) == 0 {
		return []string{"admin"}
	}
	return scopes
}

// checkKeyScopeGrant mirrors the API issuer cap: non-owners cannot mint
// scopes beyond their own grants. Empty string means allowed.
func checkKeyScopeGrant(perms store.AdminPermissions, scopes []string) string {
	for _, s := range scopes {
		switch s {
		case "admin", "*":
			return "Only owners may issue admin/* keys"
		case "billing:read", "billing:write":
			if !perms.CanManageBilling {
				return "Issuing billing scopes requires the billing permission"
			}
		case "node:read", "node:write":
			if !perms.CanManageNodes {
				return "Issuing node scopes requires the node permission"
			}
		case "user:read", "user:write":
			if !perms.CanManageUsers {
				return "Issuing user scopes requires the user permission"
			}
		case "ai":
			if !perms.CanAccessAICopilot {
				return "Issuing the ai scope requires AI Copilot access"
			}
		}
	}
	return ""
}

// POST /admin/api-keys/{id}/delete

func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	callerRole := adminCtx.Role
	if h.repos != nil && h.repos.Admins != nil {
		if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
			callerRole = callerAdmin.Role.String
		}
	}
	if callerRole != "owner" && callerRole != "superadmin" && !h.getCallerPermissions(r.Context()).CanViewAPIKeys {
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+owner+or+superadmin+role+required+to+delete+API+keys", http.StatusSeeOther)
		return
	}

	keyIDStr := chi.URLParam(r, "id")
	if keyID, err := uuid.Parse(keyIDStr); err == nil {
		_ = h.repos.APIKeys.Delete(r.Context(), keyID)
		h.recordAudit(r, "DeleteAPIKey", "api_key", &keyID, fmt.Sprintf("Revoked API key ID %s", keyIDStr))
	}
	http.Redirect(w, r, "/admin/settings?success=API+key+revoked+successfully", http.StatusSeeOther)
}

// GET /admin/qr?text=...

package web

import (
	"fmt"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"net/http"
	"strconv"
	"time"
)

func (h *Handler) PurgeOldAuditLogs(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/admin/audit?error=Forbidden:+only+owner+and+superadmin+can+purge+logs", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	replies, _ := h.repos.Billing.GetBotReplies(ctx)
	retention := store.ParseLogRetentionSettings(replies)
	days := retention.RetentionDays
	if d, err := strconv.Atoi(r.FormValue("days")); err == nil && d > 0 {
		days = d
	}

	if days > 0 {
		cutoff := time.Now().AddDate(0, 0, -days)
		if err := h.repos.AuditLogs.DeleteOlderThan(ctx, cutoff); err != nil {
			http.Redirect(w, r, "/admin/audit?error=Failed+to+purge+logs", http.StatusSeeOther)
			return
		}
		diffJSON := fmt.Sprintf(`{"days":%d,"cutoff":"%s"}`, days, cutoff.Format(time.RFC3339))
		h.recordAudit(r, "PurgeAuditLogs", "audit_logs", nil, diffJSON)
	}

	http.Redirect(w, r, "/admin/audit?success=Expired+audit+logs+purged+successfully", http.StatusSeeOther)
}

// POST /admin/telegram-bot/test

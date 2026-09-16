package web

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) Audit(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanViewAudit {
		http.Redirect(w, r, "/admin/dashboard?error=Access+denied:+Audit+trail+permission+required", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	data := h.basePageData(r, "audit")

	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	pageSize := int32(50)
	offset := int32(page-1) * pageSize

	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))
	resourceFilter := strings.TrimSpace(r.URL.Query().Get("resource"))

	logs, err := h.repos.AuditLogs.List(ctx, store.ListAuditLogsParams{
		Column1:     uuid.Nil,
		Column2:     actionFilter,
		Column3:     resourceFilter,
		CreatedAt:   pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
		CreatedAt_2: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		Limit:       pageSize,
		Offset:      offset,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to list audit logs", "error", err)
	}

	totalCount, _ := h.repos.AuditLogs.Count(ctx, store.CountAuditLogsParams{
		Column1:     uuid.Nil,
		Column2:     actionFilter,
		Column3:     resourceFilter,
		CreatedAt:   pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
		CreatedAt_2: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	})

	botReplies, _ := h.repos.Billing.GetBotReplies(ctx)
	logRetention := store.ParseLogRetentionSettings(botReplies)

	data["AuditLogs"] = logs
	data["TotalCount"] = totalCount
	data["CurrentPage"] = page
	data["ActionFilter"] = actionFilter
	data["ResourceFilter"] = resourceFilter
	data["LogRetention"] = logRetention
	data["Success"] = r.URL.Query().Get("success")
	data["Error"] = r.URL.Query().Get("error")

	_ = h.tmpl.Render(w, "audit.html", data)
}

// POST /admin/audit/purge

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

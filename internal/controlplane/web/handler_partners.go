package web

import (
	"github.com/go-chi/chi/v5"
	"fmt"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"net/http"
	"net/url"
	"strings"
)

func (h *Handler) CreatePartnerTenant(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	perms := h.getCallerPermissions(r.Context())
	callerRole := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}
	if callerRole != "owner" && callerRole != "superadmin" && !perms.CanManagePartners {
		http.Redirect(w, r, "/admin/settings/partners?error=Forbidden:+permission+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings/partners?error=invalid_form", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	slug := strings.ToLower(strings.TrimSpace(r.FormValue("slug")))
	botToken := strings.TrimSpace(r.FormValue("bot_token"))
	botUsername := strings.TrimPrefix(strings.TrimSpace(r.FormValue("bot_username")), "@")
	channelLink := strings.TrimSpace(r.FormValue("channel_link"))
	supportLink := strings.TrimSpace(r.FormValue("support_link"))
	customDomain := strings.TrimSpace(r.FormValue("custom_domain"))
	miniappURL := strings.TrimSpace(r.FormValue("miniapp_url"))
	bannerURL := strings.TrimSpace(r.FormValue("banner_url"))
	welcomeText := strings.TrimSpace(r.FormValue("welcome_text"))
	requireChannelSub := r.FormValue("require_channel_sub") == "true" || r.FormValue("require_channel_sub") == "on"

	if name == "" || slug == "" || botToken == "" {
		http.Redirect(w, r, "/admin/settings/partners?error=Name,+Slug,+and+Bot+Token+are+required", http.StatusSeeOther)
		return
	}

	t := &store.Tenant{
		Name:              name,
		Slug:              slug,
		BotToken:          botToken,
		BotUsername:       botUsername,
		ChannelLink:       channelLink,
		RequireChannelSub: requireChannelSub,
		SupportLink:       supportLink,
		CustomDomain:      customDomain,
		MiniappURL:        miniappURL,
		BannerURL:         bannerURL,
		WelcomeText:       welcomeText,
		IsActive:          true,
	}

	t.AdminID = &adminCtx.AdminID

	if h.repos.Tenants != nil {
		if err := h.repos.Tenants.Create(r.Context(), t); err != nil {
			http.Redirect(w, r, "/admin/settings/partners?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
	}

	h.recordAudit(r, "CreatePartnerTenant", "tenant", &t.ID, fmt.Sprintf("Created partner brand %s (%s)", name, slug))
	http.Redirect(w, r, "/admin/settings/partners?success=Partner+bot+and+brand+created+successfully", http.StatusSeeOther)
}

// POST /admin/settings/partners/{id}/delete

func (h *Handler) DeletePartnerTenant(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	perms := h.getCallerPermissions(r.Context())
	callerRole := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}
	if callerRole != "owner" && callerRole != "superadmin" && !perms.CanManagePartners {
		http.Redirect(w, r, "/admin/settings/partners?error=Forbidden:+permission+required", http.StatusSeeOther)
		return
	}

	idStr := chi.URLParam(r, "id")
	tenantID, err := uuid.Parse(idStr)
	if err != nil {
		http.Redirect(w, r, "/admin/settings/partners?error=Invalid+tenant+ID", http.StatusSeeOther)
		return
	}

	if h.repos.Tenants != nil {
		_ = h.repos.Tenants.Delete(r.Context(), tenantID)
	}

	h.recordAudit(r, "DeletePartnerTenant", "tenant", &tenantID, "Deleted partner brand")
	http.Redirect(w, r, "/admin/settings/partners?success=Partner+brand+removed+successfully", http.StatusSeeOther)
}

// POST /admin/settings/billing

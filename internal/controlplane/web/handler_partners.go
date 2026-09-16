package web

import (
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"net/http"
	"net/url"
	"strings"
)

func (h *Handler) SettingsPartners(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/admin/dashboard?error=Forbidden:+permission+required", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	data := h.basePageData(r, "partners")

	var partnerList []store.Tenant
	if h.repos.Tenants != nil {
		partnerList, _ = h.repos.Tenants.List(ctx)
	}

	admins, _ := h.repos.Admins.List(ctx)
	apiKeys, _ := h.repos.APIKeys.List(ctx)
	gateways, _ := h.repos.Billing.ListPaymentGateways(ctx)
	billingSettings, _ := h.repos.Billing.GetBillingSettings(ctx)
	botReplies, _ := h.repos.Billing.GetBotReplies(ctx)

	data["Admins"] = admins
	data["APIKeys"] = apiKeys
	data["Gateways"] = gateways
	data["BillingSettings"] = billingSettings
	data["BotReplies"] = botReplies
	data["Partners"] = partnerList
	data["Saved"] = r.URL.Query().Get("saved") == "true"
	data["ActiveTab"] = "partners"
	data["CanEditSettings"] = callerRole == "owner" || callerRole == "superadmin" || perms.CanManagePartners

	_ = h.tmpl.Render(w, "settings.html", data)
}

// POST /admin/settings/partners/create

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

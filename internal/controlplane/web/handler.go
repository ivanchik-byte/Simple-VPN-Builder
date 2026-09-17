package web

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/alerting"
	apimiddleware "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/skip2/go-qrcode"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type TelemetryData struct {
	CPUPercent      float64
	CPUModel        string
	RAMPercent      float64
	RAMUsed         int64
	RAMTotal        int64
	DiskPercent     float64
	DiskUsed        int64
	DiskTotal       int64
	RxSpeed         int64
	TxSpeed         int64
	TotalTraffic24h int64
	History         []MetricSnapshot
}

type StatsSummary struct {
	ActiveNodes        int
	TotalNodes         int
	ActiveUsers        int
	TotalUsers         int
	TotalTrafficBytes  int64
	SupportedProtocols int
}

type Handler struct {
	tmpl             *TemplateEngine
	repos            *store.Repositories
	jwtManager       *auth.JWTManager
	passwordManager  *auth.PasswordManager
	totpManager      *auth.TOTPManager
	apiKeyManager    *auth.APIKeyManager
	sessionMgr       *cpgrpc.SessionManager
	provisioner      *service.CredentialProvisioner
	broadcastService *service.BroadcastService
	alertDispatcher  *alerting.AlertDispatcher
	loginLimiter     *apimiddleware.RateLimiter
	copilotService   *ai.CopilotService
}

func (h *Handler) SetCopilotService(c *ai.CopilotService) {
	h.copilotService = c
}

func (h *Handler) SetLoginRateLimiter(rl *apimiddleware.RateLimiter) {
	h.loginLimiter = rl
}

func (h *Handler) SetProvisioner(p *service.CredentialProvisioner) {
	h.provisioner = p
}

func (h *Handler) SetBroadcastService(s *service.BroadcastService) {
	h.broadcastService = s
}

func (h *Handler) SetAlertDispatcher(d *alerting.AlertDispatcher) {
	h.alertDispatcher = d
}

func NewHandler(
	tmpl *TemplateEngine,
	repos *store.Repositories,
	jwtManager *auth.JWTManager,
	passwordManager *auth.PasswordManager,
	totpManager *auth.TOTPManager,
	apiKeyManager *auth.APIKeyManager,
	sessionMgr *cpgrpc.SessionManager,
) *Handler {
	return &Handler{
		tmpl:            tmpl,
		repos:           repos,
		jwtManager:      jwtManager,
		passwordManager: passwordManager,
		totpManager:     totpManager,
		apiKeyManager:   apiKeyManager,
		sessionMgr:      sessionMgr,
	}
}

func (h *Handler) getCallerPermissions(ctx context.Context) store.AdminPermissions {
	adminCtx := GetAdminContext(ctx)
	if adminCtx == nil {
		return store.DefaultAdminPermissions("admin")
	}
	if adminCtx.Role == "owner" {
		return store.DefaultAdminPermissions("owner")
	}
	if h.repos == nil || h.repos.Admins == nil {
		return store.DefaultAdminPermissions(adminCtx.Role)
	}
	callerAdmin, err := h.repos.Admins.GetByID(ctx, adminCtx.AdminID)
	if err != nil {
		return store.DefaultAdminPermissions(adminCtx.Role)
	}
	return callerAdmin.ParsedPermissions()
}

func (h *Handler) basePageData(r *http.Request, activeNav string) map[string]any {
	adminCtx := GetAdminContext(r.Context())
	username := ""
	role := ""
	adminID := ""
	if adminCtx != nil {
		username = adminCtx.Username
		role = adminCtx.Role
		adminID = adminCtx.AdminID.String()
	}
	perms := h.getCallerPermissions(r.Context())
	theme := "dark"
	if cookie, err := r.Cookie("vpn_theme"); err == nil && (cookie.Value == "light" || cookie.Value == "dark") {
		theme = cookie.Value
	}
	var csrfToken string
	if adminCtx != nil && h.jwtManager != nil {
		csrfToken = GenerateCSRFToken(adminID, h.jwtManager.SecretBytes(), 24*time.Hour)
	}

	data := map[string]any{
		"Theme":              theme,
		"ActiveNav":          activeNav,
		"AdminUsername":      username,
		"AdminRole":          role,
		"AdminID":            adminID,
		"CSRFToken":          csrfToken,
		"IsLoginPage":        false,
		"CanBroadcast":       perms.CanBroadcast,
		"CanManageUsers":     perms.CanManageUsers,
		"CanDeleteUsers":     perms.CanDeleteUsers,
		"CanResetTraffic":    perms.CanResetTraffic,
		"CanManageNodes":     perms.CanManageNodes,
		"CanManagePlans":     perms.CanManagePlans,
		"CanViewAudit":       perms.CanViewAudit,
		"CanEditBotReplies":  perms.CanEditBotReplies,
		"CanManagePartners":  perms.CanManagePartners,
		"CanAccessAICopilot": perms.CanAccessAICopilot || role == "owner",
	}
	if errStr := r.URL.Query().Get("error"); errStr != "" {
		data["Error"] = errStr
	}
	if succStr := r.URL.Query().Get("success"); succStr != "" {
		data["Success"] = succStr
	}
	return data
}

// GET /admin/login

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/login?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	loginInput := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	totpCode := strings.TrimSpace(r.FormValue("totp_code"))

	ctx := r.Context()

	// Extract remote IP for rate limiting
	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteIP = host
	}

	// 1. Dual-tier brute-force rate limit: Per-IP & Per-Account
	if h.loginLimiter != nil {
		ipKey := "login:ip:" + remoteIP
		if allowed, _, retrySec, _ := h.loginLimiter.Allow(ctx, ipKey); !allowed {
			h.recordAudit(r, "LoginRateLimited", "auth", nil, fmt.Sprintf("IP %s temporarily blocked due to excessive login attempts", remoteIP))
			http.Redirect(w, r, fmt.Sprintf("/admin/login?error=Too+many+login+attempts.+Please+wait+%d+seconds&username=%s", retrySec, url.QueryEscape(loginInput)), http.StatusSeeOther)
			return
		}
		if loginInput != "" {
			userKey := "login:user:" + strings.ToLower(loginInput)
			if allowed, _, retrySec, _ := h.loginLimiter.Allow(ctx, userKey); !allowed {
				h.recordAudit(r, "LoginAccountRateLimited", "auth", nil, fmt.Sprintf("Account %s temporarily blocked due to repeated failures", loginInput))
				http.Redirect(w, r, fmt.Sprintf("/admin/login?error=Account+temporarily+rate-limited.+Please+wait+%d+seconds&username=%s", retrySec, url.QueryEscape(loginInput)), http.StatusSeeOther)
				return
			}
		}
	}

	admin, err := h.repos.Admins.GetByEmail(ctx, loginInput)
	if err != nil && !strings.Contains(loginInput, "@") {
		// Fallback: allow logging in with short username 'admin' matching 'admin@vpnbuilder.local'
		admin, err = h.repos.Admins.GetByEmail(ctx, loginInput+"@vpnbuilder.local")
	}
	if err != nil {
		// Consume constant CPU cycles to prevent timing attacks / user enumeration
		h.passwordManager.DummyVerify(password)
		h.recordAudit(r, "LoginFailed", "auth", nil, fmt.Sprintf("Failed login attempt for unknown user: %s from IP %s", loginInput, remoteIP))
		http.Redirect(w, r, "/admin/login?error=Invalid+credentials&username="+url.QueryEscape(loginInput), http.StatusSeeOther)
		return
	}

	if err := h.passwordManager.Verify(password, admin.PasswordHash); err != nil {
		h.recordAudit(r, "LoginFailed", "auth", &admin.ID, fmt.Sprintf("Invalid password attempt for %s from IP %s", admin.Email, remoteIP))
		http.Redirect(w, r, "/admin/login?error=Invalid+credentials&username="+url.QueryEscape(loginInput), http.StatusSeeOther)
		return
	}

	if admin.TotpSecret.Valid && admin.TotpSecret.String != "" {
		if totpCode == "" || !h.totpManager.ValidateCode(totpCode, admin.TotpSecret.String) {
			h.recordAudit(r, "LoginFailed2FA", "auth", &admin.ID, fmt.Sprintf("Invalid 2FA code attempt for %s from IP %s", admin.Email, remoteIP))
			http.Redirect(w, r, "/admin/login?error=Invalid+2FA+code&username="+url.QueryEscape(loginInput), http.StatusSeeOther)
			return
		}
	}

	roleStr := "admin"
	if admin.Role.Valid {
		roleStr = admin.Role.String
	}

	// Web UI admin session uses 24-hour lifetime to match cookie expiration
	accessToken, err := h.jwtManager.GenerateAccessTokenWithTTL(admin.ID, admin.Email, roleStr, 24*time.Hour)
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=Token+generation+failed", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieAuthName,
		Value:    accessToken,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}

// POST /admin/logout

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(CookieAuthName)
	if err == nil && cookie.Value != "" {
		claims, err := h.jwtManager.ValidateAccessToken(cookie.Value)
		if err == nil && claims != nil && claims.ID != "" && h.jwtManager.Blacklist() != nil {
			_ = h.jwtManager.Blacklist().Revoke(r.Context(), claims.ID, 24*time.Hour)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieAuthName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// GET /admin/dashboard

func (h *Handler) GenerateQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" {
		http.Error(w, "missing text parameter", http.StatusBadRequest)
		return
	}

	png, err := qrcode.Encode(text, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// GET /client/{token} (deprecated & disabled to hide admin control plane from clients)

func (h *Handler) recordAudit(r *http.Request, action string, resType string, resID *uuid.UUID, details string) {
	if h.repos == nil || h.repos.AuditLogs == nil {
		return
	}

	// Check configurable audit logging category toggles
	if h.repos.Billing != nil {
		if replies, err := h.repos.Billing.GetBotReplies(r.Context()); err == nil {
			cfg := store.ParseLogRetentionSettings(replies)
			if (action == "DirectMessageUser" || action == "SendBroadcast") && !cfg.LogDirectMessages {
				return
			}
			if resType == "auth" && !cfg.LogAuth {
				return
			}
			if resType == "user" && !cfg.LogUserManagement && action != "DirectMessageUser" {
				return
			}
			if resType == "billing" && !cfg.LogBilling {
				return
			}
			if resType == "node" && !cfg.LogNodes {
				return
			}
			if (resType == "settings" || resType == "bot") && !cfg.LogSettings {
				return
			}
		}
	}

	adminCtx := GetAdminContext(r.Context())
	var adminUUID pgtype.UUID
	actorName := "System"
	if adminCtx != nil {
		adminUUID = pgtype.UUID{Bytes: adminCtx.AdminID, Valid: true}
		actorName = adminCtx.Username
	}

	var resourceUUID pgtype.UUID
	targetName := resType
	if resID != nil {
		resourceUUID = pgtype.UUID{Bytes: *resID, Valid: true}
		targetName = fmt.Sprintf("%s:%s", resType, resID.String()[:8])
	}

	ipStr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ipStr = r.RemoteAddr
	}
	var parsedIP *netip.Addr
	if addr, parseErr := netip.ParseAddr(ipStr); parseErr == nil {
		parsedIP = &addr
	}

	_, _ = h.repos.AuditLogs.Create(r.Context(), store.CreateAuditLogParams{
		AdminID:      adminUUID,
		ApiKeyID:     pgtype.UUID{Valid: false},
		Action:       action,
		ResourceType: pgtype.Text{String: resType, Valid: resType != ""},
		ResourceID:   resourceUUID,
		Diff:         []byte(details),
		IpAddress:    parsedIP,
		UserAgent:    pgtype.Text{String: r.UserAgent(), Valid: r.UserAgent() != ""},
	})

	if h.alertDispatcher != nil {
		h.alertDispatcher.SendAuditAlert(actorName, action, targetName, details)
	}
}

// POST /admin/users/{id}/ban

func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"resource not found","code":404}`))
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_ = h.tmpl.RenderStandalone(w, "error.html", map[string]any{
		"Code":    "404",
		"Title":   "Page Not Found",
		"Message": "The requested endpoint or resource was not found on this server.",
		"Accent":  "#a78bfa",
		"Glow":    "rgba(139,92,246,0.12)",
	})
}

// RenderErrorPage writes a standalone error document with the given status.

func (h *Handler) RenderErrorPage(w http.ResponseWriter, code int, title, message, accent, glow string, retryAfter int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = h.tmpl.RenderStandalone(w, "error.html", map[string]any{
		"Code":       code,
		"Title":      title,
		"Message":    message,
		"Accent":     accent,
		"Glow":       glow,
		"RetryAfter": retryAfter,
	})
}

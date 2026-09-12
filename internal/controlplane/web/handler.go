package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/alerting"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
	apimiddleware "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
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
	if adminCtx.Role == "owner" || adminCtx.Role == "superadmin" {
		return store.DefaultAdminPermissions(adminCtx.Role)
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
		"Theme":           theme,
		"ActiveNav":       activeNav,
		"AdminUsername":   username,
		"AdminRole":       role,
		"AdminID":         adminID,
		"CSRFToken":       csrfToken,
		"IsLoginPage":     false,
		"CanBroadcast":    perms.CanBroadcast,
		"CanManageUsers":  perms.CanManageUsers,
		"CanDeleteUsers":  perms.CanDeleteUsers,
		"CanResetTraffic": perms.CanResetTraffic,
		"CanManageNodes":  perms.CanManageNodes,
		"CanManagePlans":  perms.CanManagePlans,
		"CanViewAudit":    perms.CanViewAudit,
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
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	data := h.basePageData(r, "login")
	data["IsLoginPage"] = true
	if errStr := r.URL.Query().Get("error"); errStr != "" {
		data["Error"] = errStr
	}
	if u := r.URL.Query().Get("username"); u != "" {
		data["Username"] = u
	}
	_ = h.tmpl.Render(w, "login.html", data)
}

// POST /admin/login
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
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "dashboard")

	nodes, _, _ := h.repos.Nodes.List(ctx, store.NodeFilter{Limit: 50, Offset: 0})
	users, _, _ := h.repos.Users.List(ctx, store.UserFilter{Limit: 100, Offset: 0})

	activeNodes := 0
	for _, n := range nodes {
		if n.Status.String == "online" {
			activeNodes++
		}
	}

	activeUsers := 0
	for _, u := range users {
		if u.Status.String == "active" {
			activeUsers++
		}
	}

	var totalTraffic int64
	overview, err := h.repos.Traffic.GetAggregateByNode(ctx, time.Now().Add(-30*24*time.Hour), time.Now())
	if err == nil {
		for _, s := range overview {
			totalTraffic += s.TotalRx + s.TotalTx
		}
	}

	data["Nodes"] = nodes
	data["Telemetry"] = h.calculateTelemetry(ctx, 3)
	data["Stats"] = StatsSummary{
		ActiveNodes:        activeNodes,
		TotalNodes:         len(nodes),
		ActiveUsers:        activeUsers,
		TotalUsers:         len(users),
		TotalTrafficBytes:  totalTraffic,
		SupportedProtocols: 3, // WireGuard, AmneziaWG, VLESS Reality
	}

	if err := h.tmpl.Render(w, "dashboard.html", data); err != nil {
		logger.ErrorContext(ctx, "failed to render dashboard template", "error", err)
		http.Error(w, fmt.Sprintf("failed to render dashboard: %v", err), http.StatusInternalServerError)
		return
	}
}

// GET /admin/partials/telemetry
func (h *Handler) TelemetryPartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	step := 3
	if sStr := r.URL.Query().Get("step"); sStr != "" {
		if val, err := strconv.Atoi(sStr); err == nil && val > 0 && val <= 300 {
			step = val
		}
	}
	telemetry := h.calculateTelemetry(ctx, step)
	_ = h.tmpl.RenderPartial(w, "telemetry_swap.html", telemetry)
}

func (h *Handler) calculateTelemetry(_ context.Context, step int) TelemetryData {
	realCPU, cpuModel, ramUsed, ramTotal, diskUsed, diskTotal := ReadHostTelemetry()

	ramPercent := 0.0
	if ramTotal > 0 {
		ramPercent = (float64(ramUsed) / float64(ramTotal)) * 100.0
	}

	diskPercent := 0.0
	if diskTotal > 0 {
		diskPercent = (float64(diskUsed) / float64(diskTotal)) * 100.0
	}

	if step <= 0 {
		step = 1
	}

	rxRate, txRate := ReadHostNetworkRates()

	return TelemetryData{
		CPUPercent:      realCPU,
		CPUModel:        cpuModel,
		RAMPercent:      ramPercent,
		RAMUsed:         ramUsed,
		RAMTotal:        ramTotal,
		DiskPercent:     diskPercent,
		DiskUsed:        diskUsed,
		DiskTotal:       diskTotal,
		RxSpeed:         rxRate,
		TxSpeed:         txRate,
		TotalTraffic24h: 0,
		History:         GlobalTelemetryHistory.GetSampledHistory(step, 24),
	}
}

// GET /admin/nodes
func (h *Handler) Nodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "nodes")
	nodes, _, _ := h.repos.Nodes.List(ctx, store.NodeFilter{Limit: 100, Offset: 0})
	data["Nodes"] = nodes
	_ = h.tmpl.Render(w, "nodes.html", data)
}

// POST /admin/nodes
func (h *Handler) CreateNode(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageNodes {
		http.Redirect(w, r, "/admin/nodes?error=Forbidden:+permission+to+manage+nodes+is+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
		return
	}

	host := r.FormValue("host")
	port := r.FormValue("port")
	if port == "" {
		port = "443"
	}
	endpoint := host + ":" + port

	_, _ = h.repos.Nodes.Create(r.Context(), store.CreateNodeParams{
		Name:         r.FormValue("name"),
		Endpoint:     endpoint,
		GrpcEndpoint: host + ":9090",
		Region:       pgtype.Text{String: r.FormValue("region"), Valid: true},
		PublicKey:    r.FormValue("public_key"),
		Status:       pgtype.Text{String: "offline", Valid: true},
	})

	http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
}

// GET /admin/nodes/{id}
func (h *Handler) NodeDetail(w http.ResponseWriter, r *http.Request) {
	nodeIDStr := chi.URLParam(r, "id")
	nodeID, err := uuid.Parse(nodeIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	node, err := h.repos.Nodes.GetByID(ctx, nodeID)
	if err != nil {
		http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
		return
	}

	data := h.basePageData(r, "nodes")
	data["Node"] = node
	data["Telemetry"] = h.calculateTelemetry(ctx, 3)

	creds, _ := h.repos.Credentials.ListActiveByNode(ctx, nodeID)
	data["Credentials"] = creds

	_ = h.tmpl.Render(w, "node_detail.html", data)
}

// POST /admin/nodes/{id}/status
func (h *Handler) UpdateNodeStatus(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageNodes {
		http.Redirect(w, r, "/admin/nodes?error=Forbidden:+permission+to+manage+nodes+is+required", http.StatusSeeOther)
		return
	}

	nodeIDStr := chi.URLParam(r, "id")
	nodeID, err := uuid.Parse(nodeIDStr)
	if err == nil {
		status := r.FormValue("status")
		_ = h.repos.Nodes.UpdateHeartbeat(r.Context(), nodeID, status)
	}
	http.Redirect(w, r, "/admin/nodes/"+nodeIDStr, http.StatusSeeOther)
}

// POST /admin/nodes/{id}/delete
func (h *Handler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageNodes {
		http.Redirect(w, r, "/admin/nodes?error=Forbidden:+permission+to+manage+nodes+is+required", http.StatusSeeOther)
		return
	}
	adminCtx := GetAdminContext(r.Context())

	nodeIDStr := chi.URLParam(r, "id")
	nodeID, err := uuid.Parse(nodeIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/nodes?error=Invalid+node+ID", http.StatusSeeOther)
		return
	}

	targetNode, _ := h.repos.Nodes.GetByID(r.Context(), nodeID)
	nodeName := "unknown"
	if targetNode.Name != "" {
		nodeName = targetNode.Name
	}

	if err := h.repos.Nodes.Delete(r.Context(), nodeID); err != nil {
		http.Redirect(w, r, "/admin/nodes?error=Failed+to+delete+node:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "DeleteNode", "node", &nodeID, fmt.Sprintf("Node %s (%s) deleted", nodeName, nodeID.String()[:8]))

	if h.alertDispatcher != nil {
		h.alertDispatcher.SendInfraAlert(nodeName, "deleted", fmt.Sprintf("Deleted by %s", adminCtx.Username))
	}

	http.Redirect(w, r, "/admin/nodes?success=Node+deleted+successfully", http.StatusSeeOther)
}

// GET /admin/users
func (h *Handler) Users(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "users")
	users, _, _ := h.repos.Users.List(ctx, store.UserFilter{Limit: 100, Offset: 0})
	data["Users"] = users
	plans, _ := h.repos.Plans.List(ctx)
	data["Plans"] = plans
	_ = h.tmpl.Render(w, "users.html", data)
}

// POST /admin/users
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageUsers {
		http.Redirect(w, r, "/admin/users?error=Forbidden:+permission+to+manage+subscribers+is+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	if username == "" {
		http.Redirect(w, r, "/admin/users?error=Username+is+required", http.StatusSeeOther)
		return
	}

	planType := r.FormValue("plan_type")
	var planID pgtype.UUID
	var trafficLimitBytes int64
	var expiresAt pgtype.Timestamptz
	note := strings.TrimSpace(r.FormValue("note"))

	if planType == "preset" {
		planIDStr := r.FormValue("plan_id")
		if pid, err := uuid.Parse(planIDStr); err == nil {
			if plan, err := h.repos.Plans.GetByID(r.Context(), pid); err == nil {
				planID = pgtype.UUID{Bytes: pid, Valid: true}
				trafficLimitBytes = plan.TrafficLimitBytes()
				days := 30
				if d, err := strconv.Atoi(r.FormValue("duration_days")); err == nil && d > 0 {
					days = d
				}
				expiresAt = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, days), Valid: true}
			}
		}
	} else {
		// Custom / Exclusive plan
		if r.FormValue("unlimited_traffic") != "true" && r.FormValue("unlimited_traffic") != "on" {
			limitGB, _ := strconv.ParseInt(r.FormValue("traffic_limit_gb"), 10, 64)
			if limitGB > 0 {
				trafficLimitBytes = limitGB * 1024 * 1024 * 1024
			}
		}
		if r.FormValue("never_expires") != "true" && r.FormValue("never_expires") != "on" {
			days, _ := strconv.Atoi(r.FormValue("duration_days"))
			if days > 0 {
				expiresAt = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, days), Valid: true}
			}
		}
	}

	params := store.CreateUserParams{
		Username:     username,
		Email:        pgtype.Text{String: email, Valid: email != ""},
		Status:       pgtype.Text{String: "active", Valid: true},
		PlanID:       planID,
		TrafficLimit: pgtype.Int8{Int64: trafficLimitBytes, Valid: trafficLimitBytes > 0},
		ExpiresAt:    expiresAt,
		Note:         pgtype.Text{String: note, Valid: note != ""},
	}

	createdUser, err := h.repos.Users.Create(r.Context(), params)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+create+subscriber:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "CreateUser", "user", &createdUser.ID, fmt.Sprintf("Subscriber %s created with plan %s", username, planType))

	http.Redirect(w, r, "/admin/users?success=Subscriber+created+successfully", http.StatusSeeOther)
}

// POST /admin/users/{id}/reset-traffic
func (h *Handler) ResetUserTraffic(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanResetTraffic {
		http.Redirect(w, r, "/admin/users?error=Forbidden:+permission+to+reset+traffic+is+required", http.StatusSeeOther)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}
	if err := h.repos.Users.ResetTraffic(r.Context(), userID); err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+reset+traffic:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "ResetUserTraffic", "user", &userID, fmt.Sprintf("Reset traffic for user %s", userID.String()[:8]))

	http.Redirect(w, r, "/admin/users?success=Traffic+quota+reset+successfully", http.StatusSeeOther)
}

// POST /admin/users/{id}/delete
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanDeleteUsers {
		http.Redirect(w, r, "/admin/users?error=Forbidden:+permission+to+delete+subscribers+is+required.+Use+suspend+instead.", http.StatusSeeOther)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	targetUser, _ := h.repos.Users.GetByID(r.Context(), userID)
	userName := targetUser.Username
	if userName == "" {
		userName = userID.String()[:8]
	}

	if err := h.repos.Users.Delete(r.Context(), userID); err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+delete+user:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "DeleteUser", "user", &userID, fmt.Sprintf("User %s deleted permanently", userName))

	http.Redirect(w, r, "/admin/users?success=User+deleted+successfully", http.StatusSeeOther)
}

// GET /admin/plans
func (h *Handler) Plans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "plans")
	plans, _ := h.repos.Plans.List(ctx)
	data["Plans"] = plans
	_ = h.tmpl.Render(w, "plans.html", data)
}

// POST /admin/plans
func (h *Handler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManagePlans {
		http.Redirect(w, r, "/admin/plans?error=Forbidden:+permission+to+manage+plans+is+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/admin/plans?error=name_required", http.StatusSeeOther)
		return
	}

	limitGB, _ := strconv.ParseInt(r.FormValue("traffic_limit_gb"), 10, 64)
	maxDevices, _ := strconv.Atoi(r.FormValue("max_devices"))
	if maxDevices <= 0 {
		maxDevices, _ = strconv.Atoi(r.FormValue("device_limit"))
	}
	if maxDevices <= 0 {
		maxDevices = 3
	}

	trialHours, _ := strconv.Atoi(r.FormValue("trial_duration_hours"))
	priceStars, _ := strconv.Atoi(r.FormValue("price_stars"))
	isTrial := r.FormValue("is_trial") == "true" || r.FormValue("is_trial") == "on"

	protocols := r.Form["protocols"]
	if len(protocols) == 0 {
		protoStr := r.FormValue("protocols")
		if protoStr != "" {
			for _, p := range strings.Split(protoStr, ",") {
				if trimmed := strings.ToLower(strings.TrimSpace(p)); trimmed != "" {
					protocols = append(protocols, trimmed)
				}
			}
		}
	}
	var cleanProtocols []string
	allowedProtocols := map[string]bool{"wireguard": true, "amneziawg": true, "vless": true}
	for _, p := range protocols {
		lowered := strings.ToLower(strings.TrimSpace(p))
		if allowedProtocols[lowered] {
			cleanProtocols = append(cleanProtocols, lowered)
		}
	}
	if len(cleanProtocols) == 0 {
		cleanProtocols = []string{"wireguard", "amneziawg", "vless"}
	}

	price1mStr := r.FormValue("price_1m")
	if price1mStr == "" {
		price1mStr = r.FormValue("price")
	}
	if price1mStr == "" {
		price1mStr = "0"
	}

	var priceNumeric, price1mNum, price3mNum, price6mNum, price12mNum pgtype.Numeric
	_ = priceNumeric.Scan(price1mStr)
	_ = price1mNum.Scan(price1mStr)

	if p3 := r.FormValue("price_3m"); p3 != "" {
		_ = price3mNum.Scan(p3)
	}
	if p6 := r.FormValue("price_6m"); p6 != "" {
		_ = price6mNum.Scan(p6)
	}
	if p12 := r.FormValue("price_12m"); p12 != "" {
		_ = price12mNum.Scan(p12)
	}

	createdPlan, _ := h.repos.Plans.Create(r.Context(), store.CreatePlanParams{
		Name:               name,
		MonthlyPrice:       priceNumeric,
		TrafficLimit:       pgtype.Int8{Int64: limitGB * 1024 * 1024 * 1024, Valid: true},
		DeviceLimit:        pgtype.Int4{Int32: int32(maxDevices), Valid: true},
		Protocols:          cleanProtocols,
		Features:           []byte("{}"),
		IsActive:           pgtype.Bool{Bool: true, Valid: true},
		IsTrial:            pgtype.Bool{Bool: isTrial, Valid: true},
		TrialDurationHours: pgtype.Int4{Int32: int32(trialHours), Valid: trialHours > 0},
		PriceStars:         pgtype.Int4{Int32: int32(priceStars), Valid: priceStars > 0},
		MaxDevices:         pgtype.Int4{Int32: int32(maxDevices), Valid: true},
		TrafficLimitGb:     pgtype.Int4{Int32: int32(limitGB), Valid: true},
		Price1m:            price1mNum,
		Price3m:            price3mNum,
		Price6m:            price6mNum,
		Price12m:           price12mNum,
	})

	h.recordAudit(r, "CreatePlan", "plan", &createdPlan.ID, fmt.Sprintf("Service plan %s created", name))

	http.Redirect(w, r, "/admin/plans?success=Plan+created+successfully", http.StatusSeeOther)
}

// POST /admin/plans/{id}
func (h *Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManagePlans {
		http.Redirect(w, r, "/admin/plans?error=Forbidden:+permission+to+manage+plans+is+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
		return
	}

	planIDStr := chi.URLParam(r, "id")
	planID, err := uuid.Parse(planIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	existing, err := h.repos.Plans.GetByID(ctx, planID)
	if err != nil {
		http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = existing.Name
	}

	limitGB, _ := strconv.ParseInt(r.FormValue("traffic_limit_gb"), 10, 64)
	maxDevices, _ := strconv.Atoi(r.FormValue("max_devices"))
	if maxDevices <= 0 {
		maxDevices, _ = strconv.Atoi(r.FormValue("device_limit"))
	}
	if maxDevices <= 0 {
		if existing.MaxDevices.Valid && existing.MaxDevices.Int32 > 0 {
			maxDevices = int(existing.MaxDevices.Int32)
		} else {
			maxDevices = int(existing.DeviceLimit.Int32)
		}
	}

	trialHours, _ := strconv.Atoi(r.FormValue("trial_duration_hours"))
	priceStars, _ := strconv.Atoi(r.FormValue("price_stars"))
	isTrial := r.FormValue("is_trial") == "true" || r.FormValue("is_trial") == "on"

	protocols := r.Form["protocols"]
	if len(protocols) == 0 {
		protoStr := r.FormValue("protocols")
		if protoStr != "" {
			for _, p := range strings.Split(protoStr, ",") {
				if trimmed := strings.ToLower(strings.TrimSpace(p)); trimmed != "" {
					protocols = append(protocols, trimmed)
				}
			}
		}
	}
	var cleanProtocols []string
	allowedProtocols := map[string]bool{"wireguard": true, "amneziawg": true, "vless": true}
	for _, p := range protocols {
		lowered := strings.ToLower(strings.TrimSpace(p))
		if allowedProtocols[lowered] {
			cleanProtocols = append(cleanProtocols, lowered)
		}
	}
	if len(cleanProtocols) == 0 {
		cleanProtocols = existing.Protocols
	}

	price1mStr := r.FormValue("price_1m")
	if price1mStr == "" {
		price1mStr = r.FormValue("price")
	}

	priceNumeric := existing.MonthlyPrice
	price1mNum := existing.Price1m
	if price1mStr != "" {
		_ = priceNumeric.Scan(price1mStr)
		_ = price1mNum.Scan(price1mStr)
	}

	price3mNum := existing.Price3m
	if p3 := r.FormValue("price_3m"); p3 != "" {
		_ = price3mNum.Scan(p3)
	}

	price6mNum := existing.Price6m
	if p6 := r.FormValue("price_6m"); p6 != "" {
		_ = price6mNum.Scan(p6)
	}

	price12mNum := existing.Price12m
	if p12 := r.FormValue("price_12m"); p12 != "" {
		_ = price12mNum.Scan(p12)
	}

	_, _ = h.repos.Plans.Update(ctx, store.UpdatePlanParams{
		ID:                 planID,
		Name:               name,
		MonthlyPrice:       priceNumeric,
		TrafficLimit:       pgtype.Int8{Int64: limitGB * 1024 * 1024 * 1024, Valid: true},
		DeviceLimit:        pgtype.Int4{Int32: int32(maxDevices), Valid: true},
		Protocols:          cleanProtocols,
		Features:           existing.Features,
		IsActive:           existing.IsActive,
		IsTrial:            pgtype.Bool{Bool: isTrial, Valid: true},
		TrialDurationHours: pgtype.Int4{Int32: int32(trialHours), Valid: trialHours > 0},
		PriceStars:         pgtype.Int4{Int32: int32(priceStars), Valid: priceStars > 0},
		MaxDevices:         pgtype.Int4{Int32: int32(maxDevices), Valid: true},
		TrafficLimitGb:     pgtype.Int4{Int32: int32(limitGB), Valid: true},
		Price1m:            price1mNum,
		Price3m:            price3mNum,
		Price6m:            price6mNum,
		Price12m:           price12mNum,
	})

	http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
}


// POST /admin/plans/{id}/delete
func (h *Handler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManagePlans {
		http.Redirect(w, r, "/admin/plans?error=Forbidden:+permission+to+manage+plans+is+required", http.StatusSeeOther)
		return
	}

	planIDStr := chi.URLParam(r, "id")
	if planID, err := uuid.Parse(planIDStr); err == nil {
		targetPlan, _ := h.repos.Plans.GetByID(r.Context(), planID)
		_ = h.repos.Plans.Delete(r.Context(), planID)
		h.recordAudit(r, "DeletePlan", "plan", &planID, fmt.Sprintf("Service plan %s deleted", targetPlan.Name))
	}
	http.Redirect(w, r, "/admin/plans?success=Plan+deleted+successfully", http.StatusSeeOther)
}

// GET /admin/credentials
func (h *Handler) Credentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "credentials")
	// ListAll returns credentials across all nodes — ListByNode(uuid.Nil) always returned empty.
	creds, _ := h.repos.Credentials.ListAll(ctx)
	data["Credentials"] = creds
	_ = h.tmpl.Render(w, "credentials.html", data)
}

// POST /admin/credentials/{id}/rotate
func (h *Handler) RotateCredential(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageUsers {
		http.Redirect(w, r, "/admin/credentials?error=Forbidden:+permission+to+manage+credentials+is+required", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	credIDStr := chi.URLParam(r, "id")
	credID, err := uuid.Parse(credIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
		return
	}

	// Fetch the existing credential so we can decide which type to rotate.
	existing, err := h.repos.Credentials.GetByID(ctx, credID)
	if err != nil {
		http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
		return
	}

	params := store.UpdateCredentialParams{
		ID:     credID,
		Status: pgtype.Text{String: "active", Valid: true},
	}

	switch existing.Protocol {
	case "wireguard", "amneziawg":
		// Generate a real new WireGuard private/public key pair.
		newPriv, keyErr := wgtypes.GeneratePrivateKey()
		if keyErr != nil {
			logger.ErrorContext(ctx, "failed to generate WireGuard key on rotate", "error", keyErr)
			http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
			return
		}
		params.PrivateKey = pgtype.Text{String: newPriv.String(), Valid: true}
		params.PublicKey = pgtype.Text{String: newPriv.PublicKey().String(), Valid: true}
	case "vless":
		// Rotate VLESS UUID.
		newUUID := uuid.New()
		params.Uuid = pgtype.UUID{Bytes: newUUID, Valid: true}
	}

	if _, err := h.repos.Credentials.Update(ctx, params); err != nil {
		logger.ErrorContext(ctx, "failed to rotate credential", "id", credID, "error", err)
	} else {
		logger.InfoContext(ctx, "rotated credential", "id", credID, "protocol", existing.Protocol)
	}
	http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
}

// POST /admin/credentials/{id}/delete
func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageUsers {
		http.Redirect(w, r, "/admin/credentials?error=Forbidden:+permission+to+manage+credentials+is+required", http.StatusSeeOther)
		return
	}

	credIDStr := chi.URLParam(r, "id")
	if credID, err := uuid.Parse(credIDStr); err == nil {
		_ = h.repos.Credentials.Delete(r.Context(), credID)
	}
	http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
}

// GET /admin/analytics
func (h *Handler) Analytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "analytics")

	now := time.Now()
	from := now.Add(-30 * 24 * time.Hour)
	nodeStats, _ := h.repos.Traffic.GetAggregateByNode(ctx, from, now)
	auditLogs, _ := h.repos.AuditLogs.List(ctx, store.ListAuditLogsParams{Limit: 50, Offset: 0})

	var totalRx, totalTx int64
	for _, ns := range nodeStats {
		totalRx += ns.TotalRx
		totalTx += ns.TotalTx
	}

	data["Analytics"] = map[string]int64{
		"TotalRxBytes": totalRx,
		"TotalTxBytes": totalTx,
		"TotalBytes":   totalRx + totalTx,
	}
	data["AuditLogs"] = auditLogs

	_ = h.tmpl.Render(w, "analytics.html", data)
}

// GET /admin/settings
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
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
	if callerRole != "owner" && callerRole != "superadmin" {
		http.Redirect(w, r, "/admin/dashboard?error=Forbidden:+settings+are+accessible+by+owner+only", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	data := h.basePageData(r, "settings")

	admins, _ := h.repos.Admins.List(ctx)
	apiKeys, _ := h.repos.APIKeys.List(ctx)
	gateways, _ := h.repos.Billing.ListPaymentGateways(ctx)
	billingSettings, _ := h.repos.Billing.GetBillingSettings(ctx)

	data["Admins"] = admins
	data["APIKeys"] = apiKeys
	data["Gateways"] = gateways
	data["BillingSettings"] = billingSettings
	data["GeneratedKey"] = r.URL.Query().Get("generated_key")
	data["Saved"] = r.URL.Query().Get("saved") == "true"
	data["Error"] = r.URL.Query().Get("error")
	data["Success"] = r.URL.Query().Get("success")
	data["ActiveTab"] = "settings"

	currentRole := adminCtx.Role
	currentAdmin, err := h.repos.Admins.GetByID(ctx, adminCtx.AdminID)
	if err == nil {
		if currentAdmin.Role.Valid && currentAdmin.Role.String != "" {
			currentRole = currentAdmin.Role.String
		}
		data["CurrentAdmin"] = currentAdmin
		data["TotpEnabled"] = currentAdmin.TotpSecret.Valid && currentAdmin.TotpSecret.String != ""
		if !currentAdmin.TotpSecret.Valid || currentAdmin.TotpSecret.String == "" {
			secret, otpURL, err := h.totpManager.GenerateSecret(currentAdmin.Email)
			if err == nil {
				data["TOTPSecret"] = secret
				data["TOTPOTPURL"] = otpURL
			}
		}
	}
	data["CurrentAdminID"] = adminCtx.AdminID.String()
	data["CurrentRole"] = currentRole

	_ = h.tmpl.Render(w, "settings.html", data)
}

// GET /admin/settings/billing
func (h *Handler) SettingsBilling(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	callerRole := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}
	if callerRole != "owner" {
		http.Redirect(w, r, "/admin/dashboard?error=Forbidden:+billing+settings+are+accessible+by+owner+only", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	data := h.basePageData(r, "billing")

	admins, _ := h.repos.Admins.List(ctx)
	apiKeys, _ := h.repos.APIKeys.List(ctx)
	gateways, _ := h.repos.Billing.ListPaymentGateways(ctx)
	billingSettings, _ := h.repos.Billing.GetBillingSettings(ctx)

	data["Admins"] = admins
	data["APIKeys"] = apiKeys
	data["Gateways"] = gateways
	data["BillingSettings"] = billingSettings
	data["GeneratedKey"] = r.URL.Query().Get("generated_key")
	data["Saved"] = r.URL.Query().Get("saved") == "true"
	data["ActiveTab"] = "billing"

	var alertCfg alerting.AlertConfig
	if tgAlertGw, err := h.repos.Billing.GetPaymentGatewayByName(ctx, "telegram_alerts"); err == nil && tgAlertGw.ConfigEncrypted != "" {
		_ = json.Unmarshal([]byte(tgAlertGw.ConfigEncrypted), &alertCfg)
	} else if h.alertDispatcher != nil {
		alertCfg = h.alertDispatcher.GetConfig()
	}
	data["AlertConfig"] = alertCfg

	salesBotToken := ""
	if salesGw, err := h.repos.Billing.GetPaymentGatewayByName(ctx, "telegram_sales_bot"); err == nil && salesGw.ConfigEncrypted != "" {
		var botCfg map[string]string
		if err := json.Unmarshal([]byte(salesGw.ConfigEncrypted), &botCfg); err == nil {
			salesBotToken = botCfg["token"]
		}
	}
	if salesBotToken == "" {
		salesBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	}
	data["SalesBotToken"] = salesBotToken

	_ = h.tmpl.Render(w, "settings.html", data)
}

// POST /admin/settings/billing
func (h *Handler) UpdateBillingSettings(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	callerRole := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}
	if callerRole != "owner" {
		http.Redirect(w, r, "/admin/settings/billing?error=Forbidden:+only+owner+can+modify+billing+and+alert+settings", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings/billing?error=invalid_form", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	cryptoToken := strings.TrimSpace(r.FormValue("cryptobot_api_token"))
	cryptoEnabled := r.FormValue("cryptobot_enabled") == "true" || r.FormValue("cryptobot_enabled") == "on" || r.FormValue("cryptobot_enabled") == "1"
	starsEnabled := r.FormValue("telegram_stars_enabled") == "true" || r.FormValue("telegram_stars_enabled") == "on" || r.FormValue("telegram_stars_enabled") == "1"
	starsPrice, _ := strconv.Atoi(r.FormValue("stars_price_per_month"))
	if starsPrice <= 0 {
		starsPrice = 250
	}
	webhookSecret := strings.TrimSpace(r.FormValue("webhook_secret"))

	_, err := h.repos.Billing.UpsertBillingSettings(ctx, store.UpsertBillingSettingsParams{
		CryptobotApiToken:    cryptoToken,
		CryptobotEnabled:     cryptoEnabled,
		TelegramStarsEnabled: starsEnabled,
		StarsPricePerMonth:   int32(starsPrice),
		WebhookSecret:        webhookSecret,
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to update billing settings", "error", err)
	}

	cryptoCfg, _ := json.Marshal(map[string]string{"token": cryptoToken})
	_, _ = h.repos.Billing.UpsertPaymentGateway(ctx, store.UpsertPaymentGatewayParams{
		Name:            "cryptobot",
		IsEnabled:       pgtype.Bool{Bool: cryptoEnabled, Valid: true},
		ConfigEncrypted: string(cryptoCfg),
	})
	_, _ = h.repos.Billing.UpsertPaymentGateway(ctx, store.UpsertPaymentGatewayParams{
		Name:            "stars",
		IsEnabled:       pgtype.Bool{Bool: starsEnabled, Valid: true},
		ConfigEncrypted: "",
	})

	// Optional Telegram Alert Bot configuration from form
	alertBotToken := strings.TrimSpace(r.FormValue("alert_bot_token"))
	alertChatIDStr := strings.TrimSpace(r.FormValue("alert_chat_id"))
	alertChatID, _ := strconv.ParseInt(alertChatIDStr, 10, 64)
	topicInfra, _ := strconv.Atoi(r.FormValue("alert_topic_infra"))
	topicAudit, _ := strconv.Atoi(r.FormValue("alert_topic_audit"))
	topicBilling, _ := strconv.Atoi(r.FormValue("alert_topic_billing"))
	alertEnabled := r.FormValue("alert_enabled") == "true" || r.FormValue("alert_enabled") == "on"

	if alertBotToken != "" || alertChatID != 0 {
		alertCfgMap := map[string]any{
			"bot_token":     alertBotToken,
			"chat_id":       alertChatID,
			"topic_infra":   topicInfra,
			"topic_audit":   topicAudit,
			"topic_billing": topicBilling,
			"enabled":       alertEnabled,
		}
		alertCfgBytes, _ := json.Marshal(alertCfgMap)
		_, _ = h.repos.Billing.UpsertPaymentGateway(ctx, store.UpsertPaymentGatewayParams{
			Name:            "telegram_alerts",
			IsEnabled:       pgtype.Bool{Bool: alertEnabled, Valid: true},
			ConfigEncrypted: string(alertCfgBytes),
		})

		if h.alertDispatcher != nil {
			h.alertDispatcher.UpdateConfig(alerting.AlertConfig{
				BotToken:     alertBotToken,
				ChatID:       alertChatID,
				TopicInfra:   topicInfra,
				TopicAudit:   topicAudit,
				TopicBilling: topicBilling,
				Enabled:      alertEnabled,
			})
		}
	}

	salesBotToken := strings.TrimSpace(r.FormValue("sales_bot_token"))
	if salesBotToken != "" {
		botCfgBytes, _ := json.Marshal(map[string]string{"token": salesBotToken})
		_, _ = h.repos.Billing.UpsertPaymentGateway(ctx, store.UpsertPaymentGatewayParams{
			Name:            "telegram_sales_bot",
			IsEnabled:       pgtype.Bool{Bool: true, Valid: true},
			ConfigEncrypted: string(botCfgBytes),
		})
	}

	h.recordAudit(r, "UpdateBillingSettings", "billing", nil, "Updated billing gateways and alert configuration")

	http.Redirect(w, r, "/admin/settings/billing?saved=true", http.StatusSeeOther)
}

// POST /admin/gateways
func (h *Handler) UpdatePaymentGateway(w http.ResponseWriter, r *http.Request) {
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
	if callerRole != "owner" {
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+only+owner+can+modify+payment+gateways", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")
	isEnabled := r.FormValue("is_enabled") == "true" || r.FormValue("is_enabled") == "on"
	token := r.FormValue("token")

	var configStr string
	if token != "" {
		cfgMap := map[string]string{"token": token}
		configJSON, _ := json.Marshal(cfgMap)
		configStr = string(configJSON)
	}

	_, _ = h.repos.Billing.UpsertPaymentGateway(r.Context(), store.UpsertPaymentGatewayParams{
		Name:            name,
		IsEnabled:       pgtype.Bool{Bool: isEnabled, Valid: true},
		ConfigEncrypted: configStr,
	})

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}


// POST /admin/admins
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
		admins, err := h.repos.Admins.List(r.Context())
		if err != nil {
			http.Redirect(w, r, "/admin/settings?error=Failed+to+verify+owner+count", http.StatusSeeOther)
			return
		}
		ownerCount := 0
		for _, a := range admins {
			if a.Role.Valid && a.Role.String == "owner" {
				ownerCount++
			}
		}
		if ownerCount <= 1 {
			http.Redirect(w, r, "/admin/settings?error=Cannot+delete+the+sole+remaining+Owner.+Create+another+Owner+first+before+deleting+this+one.", http.StatusSeeOther)
			return
		}
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
		CanBroadcast:    r.FormValue("can_broadcast") == "on" || r.FormValue("can_broadcast") == "true",
		CanManageUsers:  r.FormValue("can_manage_users") == "on" || r.FormValue("can_manage_users") == "true",
		CanDeleteUsers:  r.FormValue("can_delete_users") == "on" || r.FormValue("can_delete_users") == "true",
		CanResetTraffic: r.FormValue("can_reset_traffic") == "on" || r.FormValue("can_reset_traffic") == "true",
		CanManageNodes:  r.FormValue("can_manage_nodes") == "on" || r.FormValue("can_manage_nodes") == "true",
		CanManagePlans:  r.FormValue("can_manage_plans") == "on" || r.FormValue("can_manage_plans") == "true",
		CanViewAudit:    r.FormValue("can_view_audit") == "on" || r.FormValue("can_view_audit") == "true",
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
	if callerRole != "owner" && callerRole != "superadmin" {
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+owner+or+superadmin+role+required+to+create+API+keys", http.StatusSeeOther)
		return
	}

	rawKey, keyHash, err := h.apiKeyManager.GenerateKey()
	if err != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	prefix := rawKey[:8]
	keyName := strings.TrimSpace(r.FormValue("name"))
	if keyName == "" {
		keyName = "API Key (" + prefix + ")"
	}
	scope := r.FormValue("scope")
	if scope == "" {
		scope = "admin"
	}
	created, createErr := h.repos.APIKeys.Create(r.Context(), store.CreateAPIKeyParams{
		Name:    keyName,
		Prefix:  prefix,
		KeyHash: keyHash,
		Scopes:  []string{scope},
	})
	if createErr != nil {
		http.Redirect(w, r, "/admin/settings?error=Failed+to+create+API+key", http.StatusSeeOther)
		return
	}

	logger.InfoContext(r.Context(), "API key created", "prefix", prefix, "id", created.ID)
	h.recordAudit(r, "CreateAPIKey", "api_key", &created.ID, fmt.Sprintf("Issued API key '%s' (prefix: %s, scope: %s)", created.Name, prefix, scope))
	http.Redirect(w, r, "/admin/settings?generated_key="+url.QueryEscape(rawKey)+"&success=API+key+issued+successfully", http.StatusSeeOther)
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
	if callerRole != "owner" && callerRole != "superadmin" {
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

// GET /client/{token}
func (h *Handler) ClientPortal(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	token, err := uuid.Parse(tokenStr)
	if err != nil {
		http.Error(w, "invalid subscription token", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, err := h.repos.Users.GetBySubscriptionToken(ctx, token)
	if err != nil {
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	subURL := fmt.Sprintf("%s://%s/sub/%s", scheme, host, token.String())

	data := map[string]any{
		"User":   user,
		"SubURL": subURL,
		"Token":  token.String(),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.tmpl.RenderStandalone(w, "portal.html", data)
}

func isClientJSONRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.Contains(contentType, "application/json") || r.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// POST /client/{token}/rotate and /client/{token}/reset
func (h *Handler) RotateClientCredentials(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	token, err := uuid.Parse(tokenStr)
	if err != nil {
		if isClientJSONRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid subscription token"})
			return
		}
		http.Error(w, "invalid subscription token", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, err := h.repos.Users.GetBySubscriptionToken(ctx, token)
	if err != nil {
		if isClientJSONRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "subscription not found"})
			return
		}
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}

	var newToken string
	if h.provisioner != nil {
		updated, rotErr := h.provisioner.RotateUserCredentials(ctx, user.ID)
		if rotErr != nil {
			logger.ErrorContext(ctx, "failed to rotate user credentials", "error", rotErr)
			if isClientJSONRequest(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to rotate credentials"})
				return
			}
			http.Error(w, "failed to rotate credentials", http.StatusInternalServerError)
			return
		}
		newToken = updated.SubscriptionToken.String()
	} else {
		updated, rotErr := h.repos.Users.RotateSubscriptionToken(ctx, user.ID)
		if rotErr != nil {
			logger.ErrorContext(ctx, "failed to rotate user subscription token", "error", rotErr)
			if isClientJSONRequest(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to rotate subscription token"})
				return
			}
			http.Error(w, "failed to rotate subscription token", http.StatusInternalServerError)
			return
		}
		newToken = updated.SubscriptionToken.String()
	}

	redirectURL := fmt.Sprintf("/client/%s", newToken)
	if isClientJSONRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"new_token":    newToken,
			"redirect_url": redirectURL,
			"message":      "Credentials rotated successfully",
		})
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// GET /client/{token}/connect
func (h *Handler) ConnectDeepLink(w http.ResponseWriter, r *http.Request) {
	tokenStr := chi.URLParam(r, "token")
	token, err := uuid.Parse(tokenStr)
	if err != nil {
		http.Error(w, "invalid subscription token", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	_, err = h.repos.Users.GetBySubscriptionToken(ctx, token)
	if err != nil {
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	subURL := fmt.Sprintf("%s://%s/sub/%s", scheme, host, token.String())
	clientApp := strings.ToLower(r.URL.Query().Get("client"))
	if clientApp == "" {
		clientApp = strings.ToLower(r.URL.Query().Get("app"))
	}

	var deepLink string
	switch clientApp {
	case "happ":
		deepLink = fmt.Sprintf("happ://add/%s", url.QueryEscape(subURL))
	case "v2rayng":
		deepLink = fmt.Sprintf("v2rayng://install-config?url=%s", url.QueryEscape(subURL))
	case "streisand":
		deepLink = fmt.Sprintf("streisand://import/%s", url.QueryEscape(subURL))
	case "singbox", "sing-box":
		deepLink = fmt.Sprintf("sing-box://import-remote-profile?url=%s#Simple-VPN", url.QueryEscape(subURL))
	case "clash", "mihomo":
		deepLink = fmt.Sprintf("clash://install-config?url=%s&name=Simple-VPN", url.QueryEscape(subURL))
	case "hiddify":
		deepLink = fmt.Sprintf("hiddify://install-sub?url=%s#Simple-VPN", url.QueryEscape(subURL))
	default:
		http.Redirect(w, r, fmt.Sprintf("/client/%s", token.String()), http.StatusSeeOther)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Launching VPN Profile</title>
  <meta http-equiv="refresh" content="0; url=%s">
  <script>window.location.href = %q;</script>
  <style>
    body { background: #0c0c0e; color: #f4f4f6; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; }
    .card { background: #131316; border: 1px solid rgba(255,255,255,0.1); border-radius: 12px; padding: 24px; text-align: center; max-width: 360px; }
    .btn { display: inline-block; margin-top: 16px; padding: 10px 18px; background: #00bb7f; color: #000; text-decoration: none; border-radius: 8px; font-weight: 600; font-size: 14px; }
    .subtext { font-size: 12px; color: #71717a; margin-top: 12px; }
  </style>
</head>
<body>
  <div class="card">
    <h3>Launching VPN Profile</h3>
    <p>Opening client application automatically...</p>
    <a href="%s" class="btn">Open App Directly</a>
    <p class="subtext"><a href="/client/%s" style="color:#71717a;">Back to Web Portal</a></p>
  </div>
</body>
</html>`, deepLink, deepLink, deepLink, token.String())
}

// GET /admin/broadcast
func (h *Handler) BroadcastPage(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanBroadcast {
		http.Redirect(w, r, "/admin/dashboard?error=Access+denied:+Telegram+broadcast+permission+required", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	data := h.basePageData(r, "broadcast")

	campaigns, err := h.repos.Billing.ListBroadcastCampaigns(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to list broadcast campaigns", "error", err)
	}
	data["Campaigns"] = campaigns
	data["Sent"] = r.URL.Query().Get("sent") == "true"
	data["Error"] = r.URL.Query().Get("error")

	_ = h.tmpl.Render(w, "broadcast.html", data)
}

// POST /admin/broadcast
func (h *Handler) CreateBroadcast(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanBroadcast {
		http.Redirect(w, r, "/admin/dashboard?error=Access+denied:+Telegram+broadcast+permission+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/broadcast?error=invalid_form", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	title := strings.TrimSpace(r.FormValue("title"))
	segment := strings.TrimSpace(r.FormValue("segment"))
	messageText := strings.TrimSpace(r.FormValue("message_text"))
	btnText := strings.TrimSpace(r.FormValue("button_text"))
	btnURL := strings.TrimSpace(r.FormValue("button_url"))

	if title == "" || messageText == "" {
		http.Redirect(w, r, "/admin/broadcast?error=missing_required_fields", http.StatusSeeOther)
		return
	}
	if segment == "" {
		segment = "all"
	}

	var buttons []service.BroadcastButton
	if btnText != "" && btnURL != "" {
		buttons = append(buttons, service.BroadcastButton{
			Text: btnText,
			URL:  btnURL,
		})
	}

	if h.broadcastService != nil {
		_, err := h.broadcastService.CreateAndDispatch(ctx, title, segment, messageText, buttons)
		if err != nil {
			logger.ErrorContext(ctx, "failed to dispatch broadcast", "error", err)
			http.Redirect(w, r, "/admin/broadcast?error=dispatch_failed", http.StatusSeeOther)
			return
		}
	} else {
		// Fallback without active broadcast service
		btnBytes, _ := json.Marshal(buttons)
		_, _ = h.repos.Billing.CreateBroadcastCampaign(ctx, store.CreateBroadcastCampaignParams{
			Title:           title,
			TargetSegment:   segment,
			MessageText:     messageText,
			InlineButtons:   btnBytes,
			TotalRecipients: pgtype.Int4{Int32: 0, Valid: true},
			Status:          pgtype.Text{String: "pending", Valid: true},
		})
	}

	http.Redirect(w, r, "/admin/broadcast?sent=true", http.StatusSeeOther)
}

func (h *Handler) recordAudit(r *http.Request, action string, resType string, resID *uuid.UUID, details string) {
	if h.repos == nil || h.repos.AuditLogs == nil {
		return
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
func (h *Handler) ToggleUserBan(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageUsers {
		http.Redirect(w, r, "/admin/users?error=Forbidden:+permission+to+manage+subscribers+is+required", http.StatusSeeOther)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	targetUser, err := h.repos.Users.GetByID(ctx, userID)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	newBannedStatus := !targetUser.IsBanned.Bool
	actionName := "BanUser"
	banReason := "Suspended by admin"
	if !newBannedStatus {
		actionName = "UnbanUser"
		banReason = ""
	}

	if err := h.repos.Users.SetBanStatus(ctx, userID, newBannedStatus, banReason); err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+update+ban+status:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, actionName, "user", &userID, fmt.Sprintf("User %s ban status set to %t", targetUser.Username, newBannedStatus))

	msg := "User+suspended+successfully"
	if !newBannedStatus {
		msg = "User+reactivated+successfully"
	}
	http.Redirect(w, r, "/admin/users?success="+msg, http.StatusSeeOther)
}

// GET /admin/audit
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

	data["AuditLogs"] = logs
	data["TotalCount"] = totalCount
	data["CurrentPage"] = page
	data["ActionFilter"] = actionFilter
	data["ResourceFilter"] = resourceFilter

	_ = h.tmpl.Render(w, "audit.html", data)
}

// POST /admin/telegram-bot/test
func (h *Handler) TestTelegramBot(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "unauthorized"})
		return
	}

	token := strings.TrimSpace(r.FormValue("token"))
	if token == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "token is required"})
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.telegram.org/bot" + token + "/getMe")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "failed to connect: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var tgResp struct {
		Ok     bool   `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tgResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid response from telegram"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if tgResp.Ok {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "username": tgResp.Result.Username})
	} else {
		errMsg := tgResp.Description
		if errMsg == "" {
			errMsg = "unauthorized token"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": errMsg})
	}
}

// NotFound serves a custom styled 404 HTML page for browser visits or JSON for API requests
func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"resource not found","code":404}`))
		return
	}
	w.WriteHeader(http.StatusNotFound)
	_ = h.tmpl.RenderStandalone(w, "404.html", nil)
}

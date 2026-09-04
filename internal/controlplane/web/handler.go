package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/skip2/go-qrcode"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
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
	tmpl            *TemplateEngine
	repos           *store.Repositories
	jwtManager      *auth.JWTManager
	passwordManager *auth.PasswordManager
	totpManager     *auth.TOTPManager
	apiKeyManager   *auth.APIKeyManager
	sessionMgr      *cpgrpc.SessionManager
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

func (h *Handler) basePageData(r *http.Request, activeNav string) map[string]any {
	adminCtx := GetAdminContext(r.Context())
	username := ""
	role := ""
	if adminCtx != nil {
		username = adminCtx.Username
		role = adminCtx.Role
	}
	return map[string]any{
		"ActiveNav":     activeNav,
		"AdminUsername": username,
		"AdminRole":     role,
		"IsLoginPage":   false,
	}
}

// GET /admin/login
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"IsLoginPage": true,
		"Error":       r.URL.Query().Get("error"),
	}
	_ = h.tmpl.Render(w, "login.html", data)
}

// POST /admin/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/login?error=Invalid+form+data", http.StatusSeeOther)
		return
	}

	email := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	totpCode := strings.TrimSpace(r.FormValue("totp_code"))

	ctx := r.Context()
	admin, err := h.repos.Admins.GetByEmail(ctx, email)
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=Invalid+credentials", http.StatusSeeOther)
		return
	}

	if err := h.passwordManager.Verify(password, admin.PasswordHash); err != nil {
		http.Redirect(w, r, "/admin/login?error=Invalid+credentials", http.StatusSeeOther)
		return
	}

	if admin.TotpSecret.Valid && admin.TotpSecret.String != "" {
		if totpCode == "" || !h.totpManager.ValidateCode(totpCode, admin.TotpSecret.String) {
			http.Redirect(w, r, "/admin/login?error=Invalid+2FA+code", http.StatusSeeOther)
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

	var totalTraffic int64
	overview, err := h.repos.Traffic.GetAggregateByNode(ctx, time.Now().Add(-30*24*time.Hour), time.Now())
	if err == nil {
		for _, s := range overview {
			totalTraffic += s.TotalRx + s.TotalTx
		}
	}

	data["Nodes"] = nodes
	data["Telemetry"] = h.calculateTelemetry(ctx)
	data["Stats"] = StatsSummary{
		ActiveNodes:        activeNodes,
		TotalNodes:         len(nodes),
		ActiveUsers:        len(users),
		TotalUsers:         len(users),
		TotalTrafficBytes:  totalTraffic,
		SupportedProtocols: 3, // WireGuard, AmneziaWG, VLESS Reality
	}

	_ = h.tmpl.Render(w, "dashboard.html", data)
}

// GET /admin/partials/telemetry
func (h *Handler) TelemetryPartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	telemetry := h.calculateTelemetry(ctx)
	_ = h.tmpl.RenderPartial(w, "telemetry_swap.html", telemetry)
}

func (h *Handler) calculateTelemetry(_ context.Context) TelemetryData {
	realCPU, cpuModel, ramUsed, ramTotal, diskUsed, diskTotal := ReadHostTelemetry()

	ramPercent := 0.0
	if ramTotal > 0 {
		ramPercent = (float64(ramUsed) / float64(ramTotal)) * 100.0
	}

	diskPercent := 0.0
	if diskTotal > 0 {
		diskPercent = (float64(diskUsed) / float64(diskTotal)) * 100.0
	}

	return TelemetryData{
		CPUPercent:      realCPU,
		CPUModel:        cpuModel,
		RAMPercent:      ramPercent,
		RAMUsed:         ramUsed,
		RAMTotal:        ramTotal,
		DiskPercent:     diskPercent,
		DiskUsed:        diskUsed,
		DiskTotal:       diskTotal,
		RxSpeed:         0,
		TxSpeed:         0,
		TotalTraffic24h: 0,
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
	data["Telemetry"] = h.calculateTelemetry(ctx)

	creds, _ := h.repos.Credentials.ListActiveByNode(ctx, nodeID)
	data["Credentials"] = creds

	_ = h.tmpl.Render(w, "node_detail.html", data)
}

// POST /admin/nodes/{id}/status
func (h *Handler) UpdateNodeStatus(w http.ResponseWriter, r *http.Request) {
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
	nodeIDStr := chi.URLParam(r, "id")
	if nodeID, err := uuid.Parse(nodeIDStr); err == nil {
		_ = h.repos.Nodes.Delete(r.Context(), nodeID)
	}
	http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
}

// GET /admin/users
func (h *Handler) Users(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "users")
	users, _, _ := h.repos.Users.List(ctx, store.UserFilter{Limit: 100, Offset: 0})
	data["Users"] = users
	_ = h.tmpl.Render(w, "users.html", data)
}

// POST /admin/users
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	limitGB, _ := strconv.ParseInt(r.FormValue("traffic_limit_gb"), 10, 64)
	limitBytes := limitGB * 1024 * 1024 * 1024

	_, _ = h.repos.Users.Create(r.Context(), store.CreateUserParams{
		Username:     r.FormValue("username"),
		Email:        pgtype.Text{String: r.FormValue("email"), Valid: true},
		Status:       pgtype.Text{String: "active", Valid: true},
		TrafficLimit: pgtype.Int8{Int64: limitBytes, Valid: limitBytes > 0},
	})

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// POST /admin/users/{id}/reset-traffic
func (h *Handler) ResetUserTraffic(w http.ResponseWriter, r *http.Request) {
	userIDStr := chi.URLParam(r, "id")
	if userID, err := uuid.Parse(userIDStr); err == nil {
		_ = h.repos.Users.ResetTraffic(r.Context(), userID)
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// POST /admin/users/{id}/delete
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	userIDStr := chi.URLParam(r, "id")
	if userID, err := uuid.Parse(userIDStr); err == nil {
		_ = h.repos.Users.Delete(r.Context(), userID)
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
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
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
		return
	}

	limitGB, _ := strconv.ParseInt(r.FormValue("traffic_limit_gb"), 10, 64)
	deviceLimit, _ := strconv.Atoi(r.FormValue("device_limit"))

	var priceNumeric pgtype.Numeric
	_ = priceNumeric.Scan(r.FormValue("price"))

	_, _ = h.repos.Plans.Create(r.Context(), store.CreatePlanParams{
		Name:         r.FormValue("name"),
		MonthlyPrice: priceNumeric,
		TrafficLimit: pgtype.Int8{Int64: limitGB * 1024 * 1024 * 1024, Valid: true},
		DeviceLimit:  pgtype.Int4{Int32: int32(deviceLimit), Valid: true},
		Protocols:    []string{"wireguard", "amneziawg", "vless"},
		IsActive:     pgtype.Bool{Bool: true, Valid: true},
	})

	http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
}

// POST /admin/plans/{id}/delete
func (h *Handler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	planIDStr := chi.URLParam(r, "id")
	if planID, err := uuid.Parse(planIDStr); err == nil {
		_ = h.repos.Plans.Delete(r.Context(), planID)
	}
	http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
}

// GET /admin/credentials
func (h *Handler) Credentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "credentials")
	creds, _ := h.repos.Credentials.ListByNode(ctx, uuid.Nil)
	data["Credentials"] = creds
	_ = h.tmpl.Render(w, "credentials.html", data)
}

// POST /admin/credentials/{id}/rotate
func (h *Handler) RotateCredential(w http.ResponseWriter, r *http.Request) {
	credIDStr := chi.URLParam(r, "id")
	if credID, err := uuid.Parse(credIDStr); err == nil {
		newKey := uuid.New().String()
		_, _ = h.repos.Credentials.Update(r.Context(), store.UpdateCredentialParams{
			ID:     credID,
			Status: pgtype.Text{String: "active", Valid: true},
		})
		logger.InfoContext(r.Context(), "Rotated credential", "id", credID, "key", newKey)
	}
	http.Redirect(w, r, "/admin/credentials", http.StatusSeeOther)
}

// POST /admin/credentials/{id}/delete
func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
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
	ctx := r.Context()
	data := h.basePageData(r, "settings")

	admins, _ := h.repos.Admins.List(ctx)
	apiKeys, _ := h.repos.APIKeys.List(ctx)

	data["Admins"] = admins
	data["APIKeys"] = apiKeys
	data["GeneratedKey"] = r.URL.Query().Get("generated_key")

	_ = h.tmpl.Render(w, "settings.html", data)
}

// POST /admin/admins
func (h *Handler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	hash, _ := h.passwordManager.Hash(r.FormValue("password"))
	_, _ = h.repos.Admins.Create(r.Context(), store.CreateAdminParams{
		Email:        r.FormValue("email"),
		PasswordHash: hash,
		Role:         pgtype.Text{String: r.FormValue("role"), Valid: true},
	})

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
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

	rawKey, keyHash, err := h.apiKeyManager.GenerateKey()
	if err != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	prefix := rawKey[:8]
	_, _ = h.repos.APIKeys.Create(r.Context(), store.CreateAPIKeyParams{
		Name:    r.FormValue("name"),
		Prefix:  prefix,
		KeyHash: keyHash,
		Scopes:  []string{r.FormValue("scope")},
	})

	http.Redirect(w, r, "/admin/settings?generated_key="+rawKey, http.StatusSeeOther)
}

// POST /admin/api-keys/{id}/delete
func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	keyIDStr := chi.URLParam(r, "id")
	if keyID, err := uuid.Parse(keyIDStr); err == nil {
		_ = h.repos.APIKeys.Delete(r.Context(), keyID)
	}
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
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

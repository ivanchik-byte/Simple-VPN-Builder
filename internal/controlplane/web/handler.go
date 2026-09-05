package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	cpgrpc "github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/grpc"
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
}

func (h *Handler) SetProvisioner(p *service.CredentialProvisioner) {
	h.provisioner = p
}

func (h *Handler) SetBroadcastService(s *service.BroadcastService) {
	h.broadcastService = s
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

	loginInput := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	totpCode := strings.TrimSpace(r.FormValue("totp_code"))

	ctx := r.Context()
	admin, err := h.repos.Admins.GetByEmail(ctx, loginInput)
	if err != nil && !strings.Contains(loginInput, "@") {
		// Fallback: allow logging in with short username 'admin' matching 'admin@vpnbuilder.local'
		admin, err = h.repos.Admins.GetByEmail(ctx, loginInput+"@vpnbuilder.local")
	}
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

	_ = h.tmpl.Render(w, "dashboard.html", data)
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

	_, _ = h.repos.Plans.Create(r.Context(), store.CreatePlanParams{
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

	http.Redirect(w, r, "/admin/plans", http.StatusSeeOther)
}

// POST /admin/plans/{id}
func (h *Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
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
	// ListAll returns credentials across all nodes — ListByNode(uuid.Nil) always returned empty.
	creds, _ := h.repos.Credentials.ListAll(ctx)
	data["Credentials"] = creds
	_ = h.tmpl.Render(w, "credentials.html", data)
}

// POST /admin/credentials/{id}/rotate
func (h *Handler) RotateCredential(w http.ResponseWriter, r *http.Request) {
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
	gateways, _ := h.repos.Billing.ListPaymentGateways(ctx)
	billingSettings, _ := h.repos.Billing.GetBillingSettings(ctx)

	data["Admins"] = admins
	data["APIKeys"] = apiKeys
	data["Gateways"] = gateways
	data["BillingSettings"] = billingSettings
	data["GeneratedKey"] = r.URL.Query().Get("generated_key")
	data["Saved"] = r.URL.Query().Get("saved") == "true"
	data["ActiveTab"] = "settings"

	_ = h.tmpl.Render(w, "settings.html", data)
}

// GET /admin/settings/billing
func (h *Handler) SettingsBilling(w http.ResponseWriter, r *http.Request) {
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

	_ = h.tmpl.Render(w, "settings.html", data)
}

// POST /admin/settings/billing
func (h *Handler) UpdateBillingSettings(w http.ResponseWriter, r *http.Request) {
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

	http.Redirect(w, r, "/admin/settings/billing?saved=true", http.StatusSeeOther)
}

// POST /admin/gateways
func (h *Handler) UpdatePaymentGateway(w http.ResponseWriter, r *http.Request) {
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
	// Only superadmins may create new admin accounts.
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil || adminCtx.Role != "superadmin" {
		http.Error(w, "forbidden: superadmin role required", http.StatusForbidden)
		return
	}

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

// POST /admin/admins/{id}/delete
func (h *Handler) DeleteAdmin(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil || adminCtx.Role != "superadmin" {
		http.Error(w, "forbidden: superadmin role required", http.StatusForbidden)
		return
	}

	adminIDStr := chi.URLParam(r, "id")
	if adminID, err := uuid.Parse(adminIDStr); err == nil {
		// Prevent deleting yourself
		if adminCtx.AdminID != adminID {
			_ = h.repos.Admins.Delete(r.Context(), adminID)
		}
	}
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
	created, createErr := h.repos.APIKeys.Create(r.Context(), store.CreateAPIKeyParams{
		Name:    r.FormValue("name"),
		Prefix:  prefix,
		KeyHash: keyHash,
		Scopes:  []string{r.FormValue("scope")},
	})
	if createErr != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}

	// Store the raw key in the session so it can be shown once on the next page.
	// We do NOT include it in the redirect URL to prevent server-log exposure.
	logger.InfoContext(r.Context(), "API key created", "prefix", prefix, "id", created.ID)
	http.Redirect(w, r, "/admin/settings?key_created="+prefix, http.StatusSeeOther)
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



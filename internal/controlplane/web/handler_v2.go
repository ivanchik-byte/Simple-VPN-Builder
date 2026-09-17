package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// CsrfToken returns a CSRF token for the authenticated admin (React console).
func (h *Handler) CsrfToken(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil || h.jwtManager == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token := GenerateCSRFToken(adminCtx.AdminID.String(), h.jwtManager.SecretBytes(), 24*time.Hour)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"csrf_token": token})
}

// AuditData returns recent audit logs for the React console.
func (h *Handler) AuditData(w http.ResponseWriter, r *http.Request) {
	if perms := h.getCallerPermissions(r.Context()); !perms.CanViewAudit {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	limit := int32(50)
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = int32(l)
	}
	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))
	resourceFilter := strings.TrimSpace(r.URL.Query().Get("resource"))
	logs, _ := h.repos.AuditLogs.List(ctx, store.ListAuditLogsParams{
		Column1:     uuid.Nil,
		Column2:     actionFilter,
		Column3:     resourceFilter,
		CreatedAt:   pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
		CreatedAt_2: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
		Limit:       limit,
		Offset:      0,
	})
	if logs == nil {
		logs = []store.AuditLog{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"logs": logs})
}
// PlanRow is a store plan plus server-computed display fields.
type PlanRow struct {
	store.Plan
	PriceStr     string   `json:"price_str"`
	Price3mStr   string   `json:"price_3m_str"`
	Price6mStr   string   `json:"price_6m_str"`
	Price12mStr  string   `json:"price_12m_str"`
	TrafficBytes int64    `json:"traffic_bytes"`
	TrafficGB    int32    `json:"traffic_gb"`
	MaxDevices   int32    `json:"max_devices"`
}

// PlansData returns service plans with display fields for the React console.
func (h *Handler) PlansData(w http.ResponseWriter, r *http.Request) {
	plans, _ := h.repos.Plans.List(r.Context())
	rows := make([]PlanRow, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, PlanRow{
			Plan:         p,
			PriceStr:     p.Price(),
			Price3mStr:   p.Price3mStr(),
			Price6mStr:   p.Price6mStr(),
			Price12mStr:  p.Price12mStr(),
			TrafficBytes: p.TrafficLimitBytes(),
			TrafficGB:    p.TrafficGB(),
			MaxDevices:   p.MaxDevicesCount(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"plans": rows})
}
// UserRow is a store user plus server-computed display fields (no secrets).
type UserRow struct {
	store.User
	DisplayName string `json:"display_name"`
	ShortID     string `json:"short_id"`
	CRMStatus   string `json:"crm_status"`
}

// UsersData returns the subscriber list with counts for the React console.
func (h *Handler) UsersData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	segment := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("segment")))
	if segment == "" {
		segment = "all"
	}

	allUsers, _, _ := h.repos.Users.List(ctx, store.UserFilter{Limit: 500, Offset: 0})

	rows := make([]UserRow, 0, len(allUsers))
	counts := map[string]int{"all": len(allUsers)}
	for _, u := range allUsers {
		u.PasswordHash = pgtype.Text{}
		st := u.CRMStatus()
		counts[st]++
		if segment == "all" || segment == st {
			rows = append(rows, UserRow{User: u, DisplayName: u.DisplayName(), ShortID: u.ShortID(), CRMStatus: st})
		}
	}

	plans, _ := h.repos.Plans.List(ctx)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"users":    rows,
		"counts":   counts,
		"segment":  segment,
		"plans":    plans,
	})
}

// maskSecret shows only the edges of a secret for display.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

// keepSecret returns the current value when the submitted one is empty or masked.
func keepSecret(submitted, current string) string {
	if submitted == "" || submitted == "***" || strings.Contains(submitted, "...") {
		return current
	}
	return submitted
}

// SafeAdmin is an admin record without secrets.
type SafeAdmin struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	TotpEnabled bool   `json:"totp_enabled"`
	Permissions []byte `json:"permissions"`
	CreatedAt   string `json:"created_at"`
}

// SafeAPIKey is an API key record without the hash.
type SafeAPIKey struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	CreatedAt string   `json:"created_at"`
}

// SafeGateway is a gateway record with masked secret values.
type SafeGateway struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	IsEnabled bool           `json:"is_enabled"`
	Config    map[string]any `json:"config"`
	Title     string         `json:"title"`
}

// SafeTenant is a tenant record with masked tokens.
type SafeTenant struct {
	store.Tenant
	BotToken         string `json:"bot_token"`
	BotWebhookSecret string `json:"bot_webhook_secret"`
}

func maskConfigValues(cfg map[string]string) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if k == "token" || k == "bot_token" || k == "secret" || strings.Contains(strings.ToLower(k), "password") || strings.Contains(strings.ToLower(k), "api_key") {
			out[k] = maskSecret(v)
			out[k+"_set"] = v != ""
			continue
		}
		out[k] = v
	}
	return out
}

// SettingsData returns all settings areas with secrets masked for the React console.
func (h *Handler) SettingsData(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	perms := h.getCallerPermissions(ctx)
	role := adminCtx.Role
	if callerAdmin, err := h.repos.Admins.GetByID(ctx, adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		role = callerAdmin.Role.String
	}

	admins, _ := h.repos.Admins.List(ctx)
	safeAdmins := make([]SafeAdmin, 0, len(admins))
	for _, a := range admins {
		safeAdmins = append(safeAdmins, SafeAdmin{
			ID:          a.ID.String(),
			Email:       a.Email,
			Role:        a.Role.String,
			TotpEnabled: a.TotpSecret.Valid && a.TotpSecret.String != "",
			Permissions: a.Permissions,
			CreatedAt:   a.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	apiKeys, _ := h.repos.APIKeys.List(ctx)
	safeKeys := make([]SafeAPIKey, 0, len(apiKeys))
	for _, k := range apiKeys {
		exp := ""
		if k.ExpiresAt.Valid {
			exp = k.ExpiresAt.Time.Format(time.RFC3339)
		}
		safeKeys = append(safeKeys, SafeAPIKey{
			ID:        k.ID.String(),
			Name:      k.Name,
			Prefix:    k.Prefix,
			Scopes:    k.Scopes,
			ExpiresAt: exp,
			CreatedAt: k.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	gateways, _ := h.repos.Billing.ListPaymentGateways(ctx)
	safeGateways := make([]SafeGateway, 0, len(gateways))
	for _, g := range gateways {
		cfg := map[string]string{}
		if g.ConfigEncrypted != "" {
			_ = json.Unmarshal([]byte(g.ConfigEncrypted), &cfg)
		}
		safeGateways = append(safeGateways, SafeGateway{
			ID:        g.ID.String(),
			Name:      g.Name,
			IsEnabled: g.IsEnabled.Bool,
			Config:    maskConfigValues(cfg),
			Title:     g.DisplayTitle(),
		})
	}

	billingSettings, _ := h.repos.Billing.GetBillingSettings(ctx)
	billing := map[string]any{
		"cryptobot_api_token":    maskSecret(billingSettings.CryptobotApiToken),
		"cryptobot_enabled":      billingSettings.CryptobotEnabled,
		"telegram_stars_enabled": billingSettings.TelegramStarsEnabled,
		"stars_price_per_month":  billingSettings.StarsPricePerMonth,
		"webhook_secret":         maskSecret(billingSettings.WebhookSecret),
	}

	botReplies, _ := h.repos.Billing.GetBotReplies(ctx)
	if botReplies == nil {
		botReplies = map[string]string{}
	}
	emailPolicy := store.ParseEmailPolicySettings(botReplies)
	email := map[string]any{
		"policy":           emailPolicy.Policy,
		"otp_enabled":      emailPolicy.OTPEnabled,
		"smtp_host":        emailPolicy.SMTPHost,
		"smtp_port":        emailPolicy.SMTPPort,
		"smtp_user":        emailPolicy.SMTPUser,
		"smtp_password":    maskSecret(emailPolicy.SMTPPassword),
		"smtp_from_email":  emailPolicy.SMTPFromEmail,
		"smtp_simulated":   emailPolicy.SMTPSimulated,
	}

	var tenants []SafeTenant
	if h.repos.Tenants != nil {
		if list, err := h.repos.Tenants.List(ctx); err == nil {
			for _, tnt := range list {
				maskedToken := maskSecret(tnt.BotToken)
				maskedHook := maskSecret(tnt.BotWebhookSecret)
				tnt.BotToken = ""
				tnt.BotWebhookSecret = ""
				tenants = append(tenants, SafeTenant{
					Tenant:           tnt,
					BotToken:         maskedToken,
					BotWebhookSecret: maskedHook,
				})
			}
		}
	}

	aiBaseURL := botReplies["ai_base_url"]
	if aiBaseURL == "" {
		aiBaseURL = "https://api.openai.com/v1"
	}
	aiModel := botReplies["ai_model"]
	if aiModel == "" {
		aiModel = "gpt-4o"
	}

	w.Header().Set("Content-Type", "application/json")
	totp := map[string]any{"enabled": false}
	if caller, err := h.repos.Admins.GetByID(ctx, adminCtx.AdminID); err == nil {
		if caller.TotpSecret.Valid && caller.TotpSecret.String != "" {
			totp["enabled"] = true
		} else if h.totpManager != nil {
			if secret, otpURL, gerr := h.totpManager.GenerateSecret(caller.Email); gerr == nil {
				totp["secret"] = secret
				totp["otpauth_url"] = otpURL
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"role":                role,
		"admin_id":            adminCtx.AdminID.String(),
		"totp":                totp,
		"can_edit_bot_replies": role == "owner" || perms.CanEditBotReplies,
		"admins":              safeAdmins,
		"api_keys":            safeKeys,
		"gateways":            safeGateways,
		"billing":             billing,
		"bot_replies":         botReplies,
		"bot_reply_categories": store.GetBotReplyCategories(),
		"referral":            store.ParseReferralSettings(botReplies),
		"email":               email,
		"tenants":             tenants,
		"ai": map[string]any{
			"base_url":  aiBaseURL,
			"model":     aiModel,
			"key_masked": maskSecret(botReplies["ai_api_key"]),
			"enabled":   botReplies["ai_enabled"] == "true",
		},
		"retention": store.ParseLogRetentionSettings(botReplies),
	})
}

// NodeData returns node details with live telemetry and credentials for the React console.
func (h *Handler) NodeData(w http.ResponseWriter, r *http.Request) {
	nodeID, err := uuid.Parse(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	node, err := h.repos.Nodes.GetByID(ctx, nodeID)
	if err != nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	var telemetry *TelemetryData
	if h.sessionMgr != nil {
		if session, ok := h.sessionMgr.Get(nodeID); ok && session != nil {
			if sys := session.GetSystemInfo(); sys != nil {
				ramPercent := 0.0
				if sys.MemoryTotal > 0 {
					ramPercent = (float64(sys.MemoryUsed) / float64(sys.MemoryTotal)) * 100.0
				}
				diskPercent := 0.0
				if sys.DiskTotal > 0 {
					diskPercent = (float64(sys.DiskUsed) / float64(sys.DiskTotal)) * 100.0
				}
				var rxSpeed, txSpeed int64
				for _, iface := range sys.Networks {
					rxSpeed += int64(iface.RxBytes)
					txSpeed += int64(iface.TxBytes)
				}
				telemetry = &TelemetryData{
					CPUPercent:  sys.CpuUsagePercent,
					RAMPercent:  ramPercent,
					RAMUsed:     int64(sys.MemoryUsed),
					RAMTotal:    int64(sys.MemoryTotal),
					DiskPercent: diskPercent,
					DiskUsed:    int64(sys.DiskUsed),
					DiskTotal:   int64(sys.DiskTotal),
					RxSpeed:     rxSpeed,
					TxSpeed:     txSpeed,
				}
			}
		}
	}

	creds, _ := h.repos.Credentials.ListActiveByNode(ctx, nodeID)
	if creds == nil {
		creds = []store.Credential{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"node":        node,
		"telemetry":   telemetry,
		"credentials": creds,
	})
}

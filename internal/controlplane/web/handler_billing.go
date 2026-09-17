package web

import (
	"github.com/go-chi/chi/v5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/alerting"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/shared/logger"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

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
	currentSettings, _ := h.repos.Billing.GetBillingSettings(ctx)
	cryptoToken := keepSecret(strings.TrimSpace(r.FormValue("cryptobot_api_token")), currentSettings.CryptobotApiToken)
	cryptoEnabled := r.FormValue("cryptobot_enabled") == "true" || r.FormValue("cryptobot_enabled") == "on" || r.FormValue("cryptobot_enabled") == "1"
	starsEnabled := r.FormValue("telegram_stars_enabled") == "true" || r.FormValue("telegram_stars_enabled") == "on" || r.FormValue("telegram_stars_enabled") == "1"
	starsPrice, _ := strconv.Atoi(r.FormValue("stars_price_per_month"))
	if starsPrice <= 0 {
		starsPrice = 250
	}
	webhookSecret := keepSecret(strings.TrimSpace(r.FormValue("webhook_secret")), currentSettings.WebhookSecret)

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
	if curGw, err := h.repos.Billing.GetPaymentGatewayByName(ctx, "telegram_alerts"); err == nil && curGw.ConfigEncrypted != "" {
		var curCfg map[string]any
		if jerr := json.Unmarshal([]byte(curGw.ConfigEncrypted), &curCfg); jerr == nil {
			if curTok, ok := curCfg["bot_token"].(string); ok {
				alertBotToken = keepSecret(alertBotToken, curTok)
			}
		}
	}
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

// POST /admin/gateways/create

func (h *Handler) CreatePaymentGateway(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/admin/settings/billing?error=Forbidden:+only+owner+can+manage+payment+gateways", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings/billing?error=invalid_form", http.StatusSeeOther)
		return
	}

	rawIdentifier := strings.ToLower(strings.TrimSpace(r.FormValue("identifier")))
	title := strings.TrimSpace(r.FormValue("title"))
	gwType := strings.ToLower(strings.TrimSpace(r.FormValue("type")))
	secretKey := strings.TrimSpace(r.FormValue("secret_key"))
	instructions := strings.TrimSpace(r.FormValue("instructions"))
	isEnabled := r.FormValue("is_enabled") == "true" || r.FormValue("is_enabled") == "on" || r.FormValue("is_enabled") == "1"

	// Strict security validation: alphanumeric slug only, length 2..32
	validIdentifier := regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
	if !validIdentifier.MatchString(rawIdentifier) {
		http.Redirect(w, r, "/admin/settings/billing?error=Invalid+gateway+identifier.+Use+2-32+characters+composed+of+letters,+numbers,+hyphens+and+underscores", http.StatusSeeOther)
		return
	}

	if title == "" {
		title = rawIdentifier
	}

	if gwType == "" {
		gwType = "webhook"
	}

	// Auto-generate strong 256-bit signing token if none provided
	if secretKey == "" && gwType == "webhook" {
		randomBytes := make([]byte, 24)
		if _, err := rand.Read(randomBytes); err == nil {
			secretKey = hex.EncodeToString(randomBytes)
		}
	}

	cfgMap := map[string]string{
		"title":        title,
		"type":         gwType,
		"token":        secretKey,
		"instructions": instructions,
	}
	configJSON, _ := json.Marshal(cfgMap)

	_, err := h.repos.Billing.UpsertPaymentGateway(r.Context(), store.UpsertPaymentGatewayParams{
		Name:            rawIdentifier,
		IsEnabled:       pgtype.Bool{Bool: isEnabled, Valid: true},
		ConfigEncrypted: string(configJSON),
	})
	if err != nil {
		logger.ErrorContext(r.Context(), "failed to create payment gateway", "error", err)
		http.Redirect(w, r, "/admin/settings/billing?error=Failed+to+save+payment+gateway", http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "CreatePaymentGateway", "billing", nil, fmt.Sprintf("Created payment gateway %s (%s)", title, rawIdentifier))
	http.Redirect(w, r, "/admin/settings/billing?saved=true", http.StatusSeeOther)
}

// POST /admin/gateways/{name}/toggle

func (h *Handler) TogglePaymentGateway(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/admin/settings/billing?error=Forbidden:+only+owner+can+modify+payment+gateways", http.StatusSeeOther)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		http.Redirect(w, r, "/admin/settings/billing?error=invalid_gateway", http.StatusSeeOther)
		return
	}

	gw, err := h.repos.Billing.GetPaymentGatewayByName(r.Context(), name)
	if err != nil {
		http.Redirect(w, r, "/admin/settings/billing?error=gateway_not_found", http.StatusSeeOther)
		return
	}

	newState := true
	if gw.IsEnabled.Valid && gw.IsEnabled.Bool {
		newState = false
	}

	_, _ = h.repos.Billing.UpsertPaymentGateway(r.Context(), store.UpsertPaymentGatewayParams{
		Name:            gw.Name,
		IsEnabled:       pgtype.Bool{Bool: newState, Valid: true},
		ConfigEncrypted: gw.ConfigEncrypted,
	})

	h.recordAudit(r, "TogglePaymentGateway", "billing", nil, fmt.Sprintf("Toggled payment gateway %s to %v", name, newState))
	http.Redirect(w, r, "/admin/settings/billing?saved=true", http.StatusSeeOther)
}

// POST /admin/gateways/{name}/delete

func (h *Handler) DeletePaymentGateway(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/admin/settings/billing?error=Forbidden:+only+owner+can+delete+payment+gateways", http.StatusSeeOther)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		http.Redirect(w, r, "/admin/settings/billing?error=invalid_gateway", http.StatusSeeOther)
		return
	}

	_ = h.repos.Billing.DeletePaymentGateway(r.Context(), name)
	h.recordAudit(r, "DeletePaymentGateway", "billing", nil, fmt.Sprintf("Deleted payment gateway %s", name))
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
		http.Redirect(w, r, "/admin/settings/billing?error=Forbidden:+only+owner+can+modify+payment+gateways", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings/billing", http.StatusSeeOther)
		return
	}

	name := r.FormValue("name")
	isEnabled := r.FormValue("is_enabled") == "true" || r.FormValue("is_enabled") == "on"

	var configStr string
	token := strings.TrimSpace(r.FormValue("token"))
	if curGw, err := h.repos.Billing.GetPaymentGatewayByName(r.Context(), name); err == nil && curGw.ConfigEncrypted != "" {
		var curCfg map[string]string
		if jerr := json.Unmarshal([]byte(curGw.ConfigEncrypted), &curCfg); jerr == nil {
			token = keepSecret(token, curCfg["token"])
		}
	}
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

	http.Redirect(w, r, "/admin/settings/billing?saved=true", http.StatusSeeOther)
}

// POST /admin/admins

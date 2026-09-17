package web

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/ai"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func saveUploadedImage(r *http.Request, fieldName string) (string, error) {
	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if header.Size == 0 {
		return "", nil
	}
	if header.Size > 15<<20 { // 15MB limit
		return "", fmt.Errorf("uploaded file is too large (max 15MB)")
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
		return "", fmt.Errorf("unsupported file format (allowed: JPG, PNG, GIF, WEBP)")
	}

	uploadDir := "./data/uploads"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create upload directory: %w", err)
	}

	filename := fmt.Sprintf("media_%d_%s%s", time.Now().Unix(), uuid.New().String()[:8], ext)
	dstPath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(dstPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", fmt.Errorf("failed to save file: %w", err)
	}

	return "/uploads/" + filename, nil
}

// POST /admin/settings/bot-replies

func (h *Handler) UpdateBotReplies(w http.ResponseWriter, r *http.Request) {
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
	if callerRole != "owner" && !perms.CanEditBotReplies {
		http.Redirect(w, r, "/admin/settings/bot-replies?error=Forbidden:+permission+required", http.StatusSeeOther)
		return
	}

	// Parse multipart form (up to 32MB) or standard urlencoded
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Redirect(w, r, "/admin/settings/bot-replies?error=Failed+to+parse+uploaded+files", http.StatusSeeOther)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/admin/settings/bot-replies?error=invalid_form", http.StatusSeeOther)
			return
		}
	}

	ctx := r.Context()

	// Optimistic concurrency: React console sends the revision it loaded.
	// Mismatched revision means another admin saved in between -> 409, no silent overwrite.
	// Legacy form UI does not send revision and is unaffected.
	if rev := r.FormValue("revision"); rev != "" {
		if cur, rerr := h.repos.Billing.GetBotRepliesRevision(ctx); rerr == nil && cur != "" && rev != cur {
			http.Error(w, "bot replies were modified by another administrator (revision mismatch), please reload the page and try again", http.StatusConflict)
			return
		}
	}

	oldReplies, _ := h.repos.Billing.GetBotReplies(ctx)
	if oldReplies == nil {
		oldReplies = map[string]string{}
	}

	// If referral program toggle was submitted from form, update it
	if r.Form.Has("reply_referral_enabled") {
		refVal := "false"
		if r.FormValue("reply_referral_enabled") == "true" || r.FormValue("reply_referral_enabled") == "on" {
			refVal = "true"
		}
		_ = h.repos.Billing.UpsertBotReply(ctx, "referral_enabled", refVal)
	}

	// Bot Token: only owner or CanEditBotReplies can update
	if r.Form.Has("bot_token") {
		_ = h.repos.Billing.UpsertBotReply(ctx, "bot_token", strings.TrimSpace(r.FormValue("bot_token")))
	}

	// Welcome banner image (support both local file upload and URL)
	if uploadedURL, err := saveUploadedImage(r, "welcome_banner_file"); err == nil && uploadedURL != "" {
		_ = h.repos.Billing.UpsertBotReply(ctx, "welcome_banner_url", uploadedURL)
	} else if r.Form.Has("welcome_banner_url") {
		_ = h.repos.Billing.UpsertBotReply(ctx, "welcome_banner_url", strings.TrimSpace(r.FormValue("welcome_banner_url")))
	}

	if r.Form.Has("channel_link") {
		_ = h.repos.Billing.UpsertBotReply(ctx, "channel_link", strings.TrimSpace(r.FormValue("channel_link")))
	}
	if r.Form.Has("support_link") {
		_ = h.repos.Billing.UpsertBotReply(ctx, "support_link", strings.TrimSpace(r.FormValue("support_link")))
	}

	for _, cat := range store.GetBotReplyCategories() {
		for _, rep := range cat.Replies {
			if !r.Form.Has("reply_" + rep.Key) {
				continue
			}
			val := strings.TrimSpace(r.FormValue("reply_" + rep.Key))
			if val == "" {
				// Empty value = reset to default: delete the override so
				// readers fall back to DefaultBotReplies / i18n bundles.
				_ = h.repos.Billing.DeleteBotReply(ctx, rep.Key)
			} else {
				_ = h.repos.Billing.UpsertBotReply(ctx, rep.Key, val)
			}

			// Check file upload first, then URL input
			if uploadedURL, err := saveUploadedImage(r, "reply_file_"+rep.Key); err == nil && uploadedURL != "" {
				_ = h.repos.Billing.UpsertBotReply(ctx, rep.Key+"_media", uploadedURL)
			} else if r.Form.Has("reply_media_" + rep.Key) {
				mediaVal := strings.TrimSpace(r.FormValue("reply_media_" + rep.Key))
				if mediaVal == "" {
					_ = h.repos.Billing.DeleteBotReply(ctx, rep.Key+"_media")
				} else {
					_ = h.repos.Billing.UpsertBotReply(ctx, rep.Key+"_media", mediaVal)
				}
			}
		}
	}

	diffMap := map[string]any{}
	for _, cat := range store.GetBotReplyCategories() {
		for _, rep := range cat.Replies {
			if !r.Form.Has("reply_" + rep.Key) {
				continue
			}
			newVal := strings.TrimSpace(r.FormValue("reply_" + rep.Key))
			if oldVal := oldReplies[rep.Key]; oldVal != newVal {
				diffMap[rep.Key] = map[string]any{"old": oldVal, "new": newVal}
			}
		}
	}
	diffJSON, _ := json.Marshal(diffMap)

	h.recordAudit(r, "UpdateBotReplies", "bot", nil, string(diffJSON))
	http.Redirect(w, r, "/admin/settings/bot-replies?saved=true", http.StatusSeeOther)
}

// GET /admin/settings/referrals

func (h *Handler) UpdateReferralSettings(w http.ResponseWriter, r *http.Request) {
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
	if callerRole != "owner" && !perms.CanEditBotReplies && !perms.CanManagePlans {
		http.Redirect(w, r, "/admin/settings/referrals?error=Forbidden:+permission+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings/referrals?error=invalid_form", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	oldReplies, _ := h.repos.Billing.GetBotReplies(ctx)
	oldSettings := store.ParseReferralSettings(oldReplies)

	enabled := r.FormValue("enabled") == "true" || r.FormValue("enabled") == "on"
	rewardModel := strings.TrimSpace(r.FormValue("reward_model"))
	if rewardModel == "" {
		rewardModel = "bonus_days"
	}
	inviterDays := 7
	if d, err := strconv.Atoi(r.FormValue("inviter_days")); err == nil && d > 0 {
		inviterDays = d
	}
	inviteeDays := 3
	if d, err := strconv.Atoi(r.FormValue("invitee_days")); err == nil && d >= 0 {
		inviteeDays = d
	}
	commissionPercent := 15
	if p, err := strconv.Atoi(r.FormValue("commission_percent")); err == nil && p >= 0 && p <= 100 {
		commissionPercent = p
	}
	minPurchaseAmount := strings.TrimSpace(r.FormValue("min_purchase_amount"))
	if minPurchaseAmount == "" {
		minPurchaseAmount = "0.00"
	}
	qualification := strings.TrimSpace(r.FormValue("qualification"))
	if qualification == "" {
		qualification = "first_payment"
	}
	dailyCap := 5
	if d, err := strconv.Atoi(r.FormValue("daily_cap")); err == nil && d > 0 {
		dailyCap = d
	}
	rewardExpired := r.FormValue("reward_expired") == "true" || r.FormValue("reward_expired") == "on"

	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	rewardExpiredStr := "false"
	if rewardExpired {
		rewardExpiredStr = "true"
	}

	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_enabled", enabledStr)
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_reward_model", rewardModel)
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_inviter_days", strconv.Itoa(inviterDays))
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_invitee_days", strconv.Itoa(inviteeDays))
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_commission_percent", strconv.Itoa(commissionPercent))
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_min_purchase_amount", minPurchaseAmount)
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_qualification", qualification)
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_daily_cap", strconv.Itoa(dailyCap))
	_ = h.repos.Billing.UpsertBotReply(ctx, "referral_reward_expired", rewardExpiredStr)

	diffMap := map[string]any{
		"enabled":             map[string]any{"old": oldSettings.Enabled, "new": enabled},
		"reward_model":        map[string]any{"old": oldSettings.RewardModel, "new": rewardModel},
		"inviter_days":        map[string]any{"old": oldSettings.InviterDays, "new": inviterDays},
		"invitee_days":        map[string]any{"old": oldSettings.InviteeDays, "new": inviteeDays},
		"commission_percent":  map[string]any{"old": oldSettings.CommissionPercent, "new": commissionPercent},
		"min_purchase_amount": map[string]any{"old": oldSettings.MinPurchaseAmount, "new": minPurchaseAmount},
		"qualification":       map[string]any{"old": oldSettings.Qualification, "new": qualification},
		"daily_cap":           map[string]any{"old": oldSettings.DailyCap, "new": dailyCap},
		"reward_expired":      map[string]any{"old": oldSettings.RewardExpired, "new": rewardExpired},
	}
	diffJSON, _ := json.Marshal(diffMap)

	h.recordAudit(r, "UpdateReferralSettings", "referral_program", nil, string(diffJSON))
	http.Redirect(w, r, "/admin/settings/referrals?saved=true", http.StatusSeeOther)
}

// POST /admin/settings/retention

func (h *Handler) UpdateLogRetentionSettings(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+only+owner+and+superadmin+can+update+retention+policy", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings?error=invalid_form", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	oldReplies, _ := h.repos.Billing.GetBotReplies(ctx)
	oldSettings := store.ParseLogRetentionSettings(oldReplies)

	retentionDays := 90
	if d, err := strconv.Atoi(r.FormValue("retention_days")); err == nil && d >= 0 {
		retentionDays = d
	}

	_ = h.repos.Billing.UpsertBotReply(ctx, "audit_log_retention_days", strconv.Itoa(retentionDays))

	logDirectMessages := r.FormValue("log_direct_messages") == "true" || r.FormValue("log_direct_messages") == "on"
	logAuth := r.FormValue("log_auth") == "true" || r.FormValue("log_auth") == "on"
	logUserMgmt := r.FormValue("log_user_mgmt") == "true" || r.FormValue("log_user_mgmt") == "on"
	logBilling := r.FormValue("log_billing") == "true" || r.FormValue("log_billing") == "on"
	logNodes := r.FormValue("log_nodes") == "true" || r.FormValue("log_nodes") == "on"
	logSettings := r.FormValue("log_settings") == "true" || r.FormValue("log_settings") == "on"

	if r.Form.Has("log_categories_submitted") {
		_ = h.repos.Billing.UpsertBotReply(ctx, "audit_log_direct_messages", strconv.FormatBool(logDirectMessages))
		_ = h.repos.Billing.UpsertBotReply(ctx, "audit_log_auth", strconv.FormatBool(logAuth))
		_ = h.repos.Billing.UpsertBotReply(ctx, "audit_log_user_mgmt", strconv.FormatBool(logUserMgmt))
		_ = h.repos.Billing.UpsertBotReply(ctx, "audit_log_billing", strconv.FormatBool(logBilling))
		_ = h.repos.Billing.UpsertBotReply(ctx, "audit_log_nodes", strconv.FormatBool(logNodes))
		_ = h.repos.Billing.UpsertBotReply(ctx, "audit_log_settings", strconv.FormatBool(logSettings))
	}

	if r.FormValue("purge_now") == "true" && retentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -retentionDays)
		_ = h.repos.AuditLogs.DeleteOlderThan(ctx, cutoff)
	}

	diffMap := map[string]any{
		"retention_days":      map[string]any{"old": oldSettings.RetentionDays, "new": retentionDays},
		"log_direct_messages": logDirectMessages,
		"log_auth":            logAuth,
		"log_user_mgmt":       logUserMgmt,
		"log_billing":         logBilling,
		"log_nodes":           logNodes,
		"log_settings":        logSettings,
	}
	diffJSON, _ := json.Marshal(diffMap)

	h.recordAudit(r, "UpdateLogRetentionPolicy", "settings", nil, string(diffJSON))
	http.Redirect(w, r, "/admin/settings?success=Audit+log+retention+and+event+filters+updated+successfully", http.StatusSeeOther)
}

// POST /admin/settings/ai

func (h *Handler) UpdateAISettings(w http.ResponseWriter, r *http.Request) {
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
	callerPerms := h.getCallerPermissions(r.Context())
	if callerRole != "owner" && !callerPerms.CanAccessAICopilot {
		http.Redirect(w, r, "/admin/settings?error=Forbidden:+only+owner+or+authorized+AI+administrators+can+update+AI+settings", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings?error=invalid_form", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	baseURL := ai.NormalizeBaseURL(r.FormValue("ai_base_url"))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(r.FormValue("ai_model"))
	if model == "" {
		model = "gpt-4o"
	}
	apiKey := strings.TrimSpace(r.FormValue("ai_api_key"))
	enabled := r.FormValue("ai_enabled") == "true" || r.FormValue("ai_enabled") == "on"

	_ = h.repos.Billing.UpsertBotReply(ctx, "ai_base_url", baseURL)
	_ = h.repos.Billing.UpsertBotReply(ctx, "ai_model", model)
	if apiKey != "" && !strings.Contains(apiKey, "...") && apiKey != "***" {
		_ = h.repos.Billing.UpsertBotReply(ctx, "ai_api_key", apiKey)
	}
	_ = h.repos.Billing.UpsertBotReply(ctx, "ai_enabled", strconv.FormatBool(enabled))

	if h.copilotService != nil {
		_ = h.copilotService.UpdateSettings(ctx, ai.AgentSettings{
			BaseURL: baseURL,
			Model:   model,
			APIKey:  apiKey,
			Enabled: enabled,
		})
	}

	h.recordAudit(r, "UpdateAISettings", "settings", nil, fmt.Sprintf(`{"base_url":"%s","model":"%s","enabled":%v}`, baseURL, model, enabled))
	http.Redirect(w, r, "/admin/settings?success=AI+Copilot+configuration+saved+successfully", http.StatusSeeOther)
}

// POST /admin/settings/ai/test

func (h *Handler) TestAIConnection(w http.ResponseWriter, r *http.Request) {
	adminCtx := GetAdminContext(r.Context())
	if adminCtx == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Unauthorized"})
		return
	}

	callerRole := adminCtx.Role
	if h.repos != nil && h.repos.Admins != nil {
		if callerAdmin, err := h.repos.Admins.GetByID(r.Context(), adminCtx.AdminID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
			callerRole = callerAdmin.Role.String
		}
	}
	callerPerms := h.getCallerPermissions(r.Context())
	if callerRole != "owner" && !callerPerms.CanAccessAICopilot {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Forbidden: insufficient permissions"})
		return
	}

	if h.copilotService == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "AI Copilot service not available"})
		return
	}

	var req ai.AgentSettings
	// Check if JSON request body or form
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&req)
	} else {
		_ = r.ParseForm()
		req.BaseURL = r.FormValue("ai_base_url")
		req.Model = r.FormValue("ai_model")
		req.APIKey = r.FormValue("ai_api_key")
	}

	duration, reply, err := h.copilotService.TestConnection(r.Context(), req)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"duration_ms": duration.Milliseconds(),
		"reply":       reply,
	})
}

// GET /admin/settings/security

func (h *Handler) UpdateEmailPolicySettings(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/admin/settings/security?error=Forbidden:+only+owner+and+superadmin+can+update+security+policies", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings/security?error=invalid_form", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	oldReplies, _ := h.repos.Billing.GetBotReplies(ctx)
	oldSettings := store.ParseEmailPolicySettings(oldReplies)

	policy := strings.TrimSpace(r.FormValue("email_policy"))
	if policy == "" {
		policy = "optional"
	}
	switch policy {
	case "optional", "required", "disabled":
		// valid
	default:
		http.Error(w, "invalid email_policy: must be one of optional, required, disabled", http.StatusBadRequest)
		return
	}
	otpEnabled := r.FormValue("email_otp_enabled") == "true" || r.FormValue("email_otp_enabled") == "on"
	smtpHost := strings.TrimSpace(r.FormValue("smtp_host"))
	smtpPort := strings.TrimSpace(r.FormValue("smtp_port"))
	smtpUser := strings.TrimSpace(r.FormValue("smtp_user"))
	smtpPassword := keepSecret(strings.TrimSpace(r.FormValue("smtp_password")), oldSettings.SMTPPassword)
	smtpFromEmail := strings.TrimSpace(r.FormValue("smtp_from_email"))
	smtpSimulated := r.FormValue("smtp_simulated") == "true" || r.FormValue("smtp_simulated") == "on"

	_ = h.repos.Billing.UpsertBotReply(ctx, "email_policy", policy)
	otpStr := "false"
	if otpEnabled {
		otpStr = "true"
	}
	_ = h.repos.Billing.UpsertBotReply(ctx, "email_otp_enabled", otpStr)
	_ = h.repos.Billing.UpsertBotReply(ctx, "smtp_host", smtpHost)
	_ = h.repos.Billing.UpsertBotReply(ctx, "smtp_port", smtpPort)
	_ = h.repos.Billing.UpsertBotReply(ctx, "smtp_user", smtpUser)
	if smtpPassword != "" {
		_ = h.repos.Billing.UpsertBotReply(ctx, "smtp_password", smtpPassword)
	}
	_ = h.repos.Billing.UpsertBotReply(ctx, "smtp_from_email", smtpFromEmail)
	simStr := "false"
	if smtpSimulated {
		simStr = "true"
	}
	_ = h.repos.Billing.UpsertBotReply(ctx, "smtp_simulated", simStr)

	diffMap := map[string]any{
		"email_policy": map[string]any{"old": oldSettings.Policy, "new": policy},
		"otp_enabled":  map[string]any{"old": oldSettings.OTPEnabled, "new": otpEnabled},
		"simulated":    map[string]any{"old": oldSettings.SMTPSimulated, "new": smtpSimulated},
	}
	diffJSON, _ := json.Marshal(diffMap)
	h.recordAudit(r, "UpdateEmailPolicySettings", "settings", nil, string(diffJSON))

	http.Redirect(w, r, "/admin/settings/security?success=Email+policy+and+security+settings+saved+successfully", http.StatusSeeOther)
}

// GET /admin/settings/partners

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
		Ok     bool `json:"ok"`
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

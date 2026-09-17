package web

import (
	"github.com/go-chi/chi/v5"
	"fmt"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
				if plan.IsTrial.Bool {
					hours := 24
					if plan.TrialDurationHours.Valid && plan.TrialDurationHours.Int32 > 0 {
						hours = int(plan.TrialDurationHours.Int32)
					}
					expiresAt = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(hours) * time.Hour), Valid: true}
				} else {
					days := 30
					if d, err := strconv.Atoi(r.FormValue("duration_days")); err == nil && d > 0 {
						days = d
					}
					expiresAt = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, days), Valid: true}
				}
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

	// Auto-provision cryptographic credentials across all active nodes
	if h.provisioner != nil {
		_ = h.provisioner.ProvisionUser(r.Context(), createdUser.ID)
	}

	tgIDStr := strings.TrimSpace(r.FormValue("telegram_id"))
	tgUsername := strings.TrimPrefix(strings.TrimSpace(r.FormValue("telegram_username")), "@")
	if tgIDStr != "" || tgUsername != "" {
		tgID, _ := strconv.ParseInt(tgIDStr, 10, 64)
		_, _ = h.repos.Users.UpdateTelegramMetadata(r.Context(), createdUser.ID, tgID, tgUsername, false, nil, "")
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

	if h.provisioner != nil {
		if err := h.provisioner.RevokeUser(r.Context(), userID); err != nil {
			http.Redirect(w, r, "/admin/users?error=Failed+to+revoke+credentials:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
	}
	if err := h.repos.Users.Delete(r.Context(), userID); err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+delete+user:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "DeleteUser", "user", &userID, fmt.Sprintf("User %s deleted permanently", userName))

	http.Redirect(w, r, "/admin/users?success=User+deleted+successfully", http.StatusSeeOther)
}

// POST /admin/users/{id}/message

func (h *Handler) DirectMessageUser(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanBroadcast {
		http.Redirect(w, r, "/admin/users?error=Forbidden:+permission+to+send+messages+is+required", http.StatusSeeOther)
		return
	}

	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users?error=invalid_form", http.StatusSeeOther)
		return
	}

	messageText := strings.TrimSpace(r.FormValue("message_text"))
	if messageText == "" {
		http.Redirect(w, r, "/admin/users?error=Message+text+cannot+be+empty", http.StatusSeeOther)
		return
	}

	btnText := strings.TrimSpace(r.FormValue("button_text"))
	btnURL := strings.TrimSpace(r.FormValue("button_url"))
	var buttons []service.BroadcastButton
	if btnText != "" && btnURL != "" {
		buttons = append(buttons, service.BroadcastButton{
			Text: btnText,
			URL:  btnURL,
		})
	}

	targetUser, err := h.repos.Users.GetByID(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	if !targetUser.TelegramID.Valid || targetUser.TelegramID.Int64 <= 0 {
		http.Redirect(w, r, "/admin/users?error=User+has+no+linked+Telegram+account", http.StatusSeeOther)
		return
	}

	if h.broadcastService == nil {
		http.Redirect(w, r, "/admin/users?error=Telegram+delivery+service+not+available", http.StatusSeeOther)
		return
	}

	err = h.broadcastService.SendDirectMessage(r.Context(), targetUser.TelegramID.Int64, messageText, buttons)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+send+message:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "DirectMessageUser", "user", &userID, fmt.Sprintf("Direct Telegram message sent to user %s (%d)", targetUser.Username, targetUser.TelegramID.Int64))
	http.Redirect(w, r, "/admin/users?success=Personal+message+sent+to+Telegram+successfully", http.StatusSeeOther)
}

// POST /admin/users/{id}/assign-plan

func (h *Handler) AssignUserPlan(w http.ResponseWriter, r *http.Request) {
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

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users?error=invalid_form", http.StatusSeeOther)
		return
	}

	planIDStr := strings.TrimSpace(r.FormValue("plan_id"))
	planID, err := uuid.Parse(planIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Please+select+a+valid+plan", http.StatusSeeOther)
		return
	}

	plan, err := h.repos.Plans.GetByID(r.Context(), planID)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Plan+not+found", http.StatusSeeOther)
		return
	}

	durationDays, _ := strconv.Atoi(r.FormValue("duration_days"))
	if durationDays <= 0 {
		durationDays = 30
	}
	expiresAt := time.Now().AddDate(0, 0, durationDays)
	trafficLimit := plan.TrafficLimitBytes()

	_, err = h.repos.Users.Update(r.Context(), store.UpdateUserParams{
		ID:           userID,
		PlanID:       pgtype.UUID{Bytes: planID, Valid: true},
		TrafficLimit: pgtype.Int8{Int64: trafficLimit, Valid: trafficLimit > 0},
		ExpiresAt:    pgtype.Timestamptz{Time: expiresAt, Valid: true},
		Status:       pgtype.Text{String: "active", Valid: true},
	})
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+assign+plan:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Auto-provision or update credentials on nodes
	if h.provisioner != nil {
		_ = h.provisioner.ProvisionUser(r.Context(), userID)
	}

	targetUser, _ := h.repos.Users.GetByID(r.Context(), userID)
	// Optionally notify user via Telegram if linked
	if targetUser.TelegramID.Valid && targetUser.TelegramID.Int64 > 0 && h.broadcastService != nil && (r.FormValue("notify_user") == "true" || r.FormValue("notify_user") == "on") {
		msg := fmt.Sprintf("Your subscription has been activated!\nPlan: <b>%s</b>\nValid for: <b>%d days</b>\nBandwidth: <b>%s</b>",
			plan.Name, durationDays, FormatBytes(trafficLimit))
		_ = h.broadcastService.SendDirectMessage(r.Context(), targetUser.TelegramID.Int64, msg, nil)
	}

	h.recordAudit(r, "AssignUserPlan", "user", &userID, fmt.Sprintf("Assigned plan %s (%d days) to user %s", plan.Name, durationDays, targetUser.Username))
	http.Redirect(w, r, "/admin/users?success=Plan+assigned+and+provisioned+successfully", http.StatusSeeOther)
}

// POST /admin/users/{id}/email

func (h *Handler) UpdateUserEmail(w http.ResponseWriter, r *http.Request) {
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

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/users?error=invalid_form", http.StatusSeeOther)
		return
	}

	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	targetUser, err := h.repos.Users.GetByID(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=User+not+found", http.StatusSeeOther)
		return
	}

	if email != "" {
		if existing, err := h.repos.Users.GetByEmail(r.Context(), email); err == nil && existing.ID != userID {
			http.Redirect(w, r, "/admin/users?error=This+email+is+already+used+by+another+subscriber", http.StatusSeeOther)
			return
		}
	}

	_, err = h.repos.Users.UpdateEmail(r.Context(), userID, email)
	if err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+update+email:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "UpdateUserEmail", "user", &userID, fmt.Sprintf("Updated email for user %s to %s", targetUser.Username, email))
	http.Redirect(w, r, "/admin/users?success=Email+updated+successfully", http.StatusSeeOther)
}

// GET /admin/plans

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

	isCurrentlyBanned := targetUser.IsBanned.Valid && targetUser.IsBanned.Bool
	newBannedStatus := !isCurrentlyBanned
	actionName := "BanUser"
	banReason := "Banned by admin"
	if !newBannedStatus {
		actionName = "UnbanUser"
		banReason = ""
	}

	if err := h.repos.Users.SetBanStatus(ctx, userID, newBannedStatus, banReason); err != nil {
		http.Redirect(w, r, "/admin/users?error=Failed+to+update+ban+status:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, actionName, "user", &userID, fmt.Sprintf("User %s ban status set to %t", targetUser.Username, newBannedStatus))

	msg := "User+banned+successfully"
	if !newBannedStatus {
		msg = "User+unbanned+successfully"
	}
	http.Redirect(w, r, "/admin/users?success="+msg, http.StatusSeeOther)
}

// GET /admin/audit

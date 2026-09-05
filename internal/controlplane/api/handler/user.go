package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/service"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type UserHandler struct {
	repo        store.UserRepository
	planRepo    store.PlanRepository
	provisioner *service.CredentialProvisioner
	audit       *middleware.AuditService
}

func NewUserHandler(repo store.UserRepository, planRepo store.PlanRepository, audit *middleware.AuditService) *UserHandler {
	return &UserHandler{
		repo:     repo,
		planRepo: planRepo,
		audit:    audit,
	}
}

func (h *UserHandler) SetProvisioner(p *service.CredentialProvisioner) {
	h.provisioner = p
}

type CreateUserRequest struct {
	Email        string     `json:"email" validate:"required,email"`
	Username     string     `json:"username" validate:"required,min=3,max=64"`
	PlanID       *uuid.UUID `json:"plan_id,omitempty" validate:"omitempty,uuid"`
	TrafficLimit int64      `json:"traffic_limit" validate:"min=0"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Note         string     `json:"note,omitempty" validate:"max=500"`
}

type UpdateUserRequest struct {
	Email        string     `json:"email" validate:"omitempty,email"`
	Username     string     `json:"username" validate:"omitempty,min=3,max=64"`
	Status       string     `json:"status" validate:"omitempty,oneof=active suspended expired"`
	PlanID       *uuid.UUID `json:"plan_id,omitempty" validate:"omitempty,uuid"`
	TrafficLimit *int64     `json:"traffic_limit,omitempty" validate:"omitempty,min=0"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Note         *string    `json:"note,omitempty" validate:"omitempty,max=500"`
}

func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	pagination := request.ParsePagination(r)
	status := r.URL.Query().Get("status")

	var planID *uuid.UUID
	if planStr := r.URL.Query().Get("plan_id"); planStr != "" {
		if parsed, err := uuid.Parse(planStr); err == nil {
			planID = &parsed
		}
	}

	filter := store.UserFilter{
		Status: status,
		PlanID: planID,
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	}

	users, total, err := h.repo.List(r.Context(), filter)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve users")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(request.NewPaginatedResponse(users, pagination, total))
}

func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Username = strings.TrimSpace(req.Username)

	var planUUID pgtype.UUID
	trafficLimit := req.TrafficLimit

	if req.PlanID != nil {
		plan, err := h.planRepo.GetByID(r.Context(), *req.PlanID)
		if err != nil {
			response.RespondBadRequest(w, r, "Specified plan does not exist", map[string]string{"plan_id": "not found"})
			return
		}
		planUUID = pgtype.UUID{Bytes: plan.ID, Valid: true}
		if trafficLimit == 0 && plan.TrafficLimit.Valid {
			trafficLimit = plan.TrafficLimit.Int64
		}
	}

	var expTime pgtype.Timestamptz
	if req.ExpiresAt != nil {
		expTime = pgtype.Timestamptz{Time: *req.ExpiresAt, Valid: true}
	}

	user, err := h.repo.Create(r.Context(), store.CreateUserParams{
		Email:        pgtype.Text{String: req.Email, Valid: req.Email != ""},
		Username:     req.Username,
		PasswordHash: pgtype.Text{},
		Status:       pgtype.Text{String: "active", Valid: true},
		PlanID:       planUUID,
		TrafficLimit: pgtype.Int8{Int64: trafficLimit, Valid: true},
		TrafficUsed:  pgtype.Int8{Int64: 0, Valid: true},
		ExpiresAt:    expTime,
		Note:         pgtype.Text{String: req.Note, Valid: req.Note != ""},
	})
	if err != nil {
		response.RespondConflict(w, r, "User with this email or username already exists")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "create", "user", &user.ID, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(user)
}

func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	user, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "User not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(user)
}

func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	existing, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "User not found")
		return
	}

	email := existing.Email
	if req.Email != "" {
		email = pgtype.Text{String: strings.TrimSpace(strings.ToLower(req.Email)), Valid: true}
	}
	username := existing.Username
	if req.Username != "" {
		username = strings.TrimSpace(req.Username)
	}
	status := existing.Status
	if req.Status != "" {
		status = pgtype.Text{String: req.Status, Valid: true}
	}
	planID := existing.PlanID
	if req.PlanID != nil {
		planID = pgtype.UUID{Bytes: *req.PlanID, Valid: true}
	}
	trafficLimit := existing.TrafficLimit
	if req.TrafficLimit != nil {
		trafficLimit = pgtype.Int8{Int64: *req.TrafficLimit, Valid: true}
	}
	expiresAt := existing.ExpiresAt
	if req.ExpiresAt != nil {
		expiresAt = pgtype.Timestamptz{Time: *req.ExpiresAt, Valid: true}
	}
	note := existing.Note
	if req.Note != nil {
		note = pgtype.Text{String: *req.Note, Valid: *req.Note != ""}
	}

	updated, err := h.repo.Update(r.Context(), store.UpdateUserParams{
		ID:           id,
		Email:        email,
		Username:     username,
		PasswordHash: existing.PasswordHash,
		Status:       status,
		PlanID:       planID,
		TrafficLimit: trafficLimit,
		ExpiresAt:    expiresAt,
		Note:         note,
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to update user")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "update", "user", &id, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		response.RespondNotFound(w, r, "User not found")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		response.RespondInternalError(w, r, "Failed to delete user")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "delete", "user", &id, nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *UserHandler) ResetTraffic(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		response.RespondNotFound(w, r, "User not found")
		return
	}

	if err := h.repo.ResetTraffic(r.Context(), id); err != nil {
		response.RespondInternalError(w, r, "Failed to reset traffic")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "reset_traffic", "user", &id, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "traffic_reset"})
}

type SubscriptionResponse struct {
	SubscriptionToken string `json:"subscription_token"`
	SubscriptionURL   string `json:"subscription_url"`
}

func (h *UserHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	user, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "User not found")
		return
	}

	token := user.SubscriptionToken.String()
	subURL := fmt.Sprintf("/sub/%s", token)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(SubscriptionResponse{
		SubscriptionToken: token,
		SubscriptionURL:   subURL,
	})
}

func (h *UserHandler) RotateSubscription(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	updated, err := h.repo.RotateSubscriptionToken(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "User not found")
		return
	}

	token := updated.SubscriptionToken.String()
	subURL := fmt.Sprintf("/sub/%s", token)

	if h.audit != nil {
		_ = h.audit.Log(r, "rotate_subscription", "user", &id, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(SubscriptionResponse{
		SubscriptionToken: token,
		SubscriptionURL:   subURL,
	})
}

type CreateTrialUserRequest struct {
	TelegramID       int64  `json:"telegram_id" validate:"required"`
	TelegramUsername string `json:"telegram_username,omitempty"`
	ReferrerCode     string `json:"referrer_code,omitempty"`
}

func (h *UserHandler) CreateTrial(w http.ResponseWriter, r *http.Request) {
	var req CreateTrialUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if req.TelegramID <= 0 {
		response.RespondBadRequest(w, r, "Invalid telegram_id", map[string]string{"telegram_id": "must be a positive integer"})
		return
	}

	ctx := r.Context()

	// Check if user with this Telegram ID already exists
	existingUser, err := h.repo.GetByTelegramID(ctx, req.TelegramID)
	if err == nil {
		if existingUser.TrialUsed.Bool {
			response.RespondConflict(w, r, "Free trial already claimed for this Telegram account")
			return
		}
		// User exists but hasn't used trial yet
		token := existingUser.SubscriptionToken.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"user":               existingUser,
			"subscription_token": token,
			"subscription_url":   fmt.Sprintf("/sub/%s", token),
		})
		return
	}

	// Fetch active trial plan from database
	trialPlan, err := h.planRepo.GetTrial(ctx)
	if err != nil {
		response.RespondNotFound(w, r, "No active free trial plan configured by operator")
		return
	}

	durationHours := int32(24)
	if trialPlan.TrialDurationHours.Valid && trialPlan.TrialDurationHours.Int32 > 0 {
		durationHours = trialPlan.TrialDurationHours.Int32
	}
	expiresAt := time.Now().Add(time.Duration(durationHours) * time.Hour)

	var trLimit int64
	if trialPlan.TrafficLimit.Valid {
		trLimit = trialPlan.TrafficLimit.Int64
	}

	var referrerID *uuid.UUID
	if strings.TrimSpace(req.ReferrerCode) != "" {
		if refUser, err := h.repo.GetByReferralCode(ctx, strings.TrimSpace(req.ReferrerCode)); err == nil {
			referrerID = &refUser.ID
		}
	}

	username := fmt.Sprintf("tg_%d", req.TelegramID)
	email := fmt.Sprintf("tg_%d@t.me", req.TelegramID)
	refCode := fmt.Sprintf("ref_%d", req.TelegramID)

	var refUUID pgtype.UUID
	if referrerID != nil {
		refUUID = pgtype.UUID{Bytes: *referrerID, Valid: true}
	}

	user, err := h.repo.Create(ctx, store.CreateUserParams{
		Email:        pgtype.Text{String: email, Valid: true},
		Username:     username,
		Status:       pgtype.Text{String: "active", Valid: true},
		PlanID:       pgtype.UUID{Bytes: trialPlan.ID, Valid: true},
		TrafficLimit: pgtype.Int8{Int64: trLimit, Valid: true},
		ExpiresAt:    pgtype.Timestamptz{Time: expiresAt, Valid: true},
		Note:         pgtype.Text{String: fmt.Sprintf("Telegram user @%s", req.TelegramUsername), Valid: true},
	})
	if err != nil {
		response.RespondInternalError(w, r, fmt.Sprintf("Failed to create trial user: %v", err))
		return
	}

	// Set trial flags and telegram metadata directly in user table
	_, _ = h.repo.Update(ctx, store.UpdateUserParams{
		ID:           user.ID,
		Email:        user.Email,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Status:       user.Status,
		PlanID:       user.PlanID,
		TrafficLimit: user.TrafficLimit,
		ExpiresAt:    user.ExpiresAt,
		Note:         user.Note,
	})

	// Provision credentials across active nodes
	if h.provisioner != nil {
		_ = h.provisioner.ProvisionUser(ctx, user.ID)
	}

	token := user.SubscriptionToken.String()
	subURL := fmt.Sprintf("/sub/%s", token)

	if h.audit != nil {
		diffBytes, _ := json.Marshal(map[string]interface{}{
			"telegram_id": req.TelegramID,
			"plan_id":     trialPlan.ID,
			"ref_id":      refUUID,
			"ref_code":    refCode,
		})
		_ = h.audit.Log(r, "claim_trial", "user", &user.ID, diffBytes)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user":               user,
		"subscription_token": token,
		"subscription_url":   subURL,
		"trial_hours":        durationHours,
		"traffic_limit_bytes": trLimit,
	})
}

func (h *UserHandler) RotateKeys(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	ctx := r.Context()
	var token string
	if h.provisioner != nil {
		updated, err := h.provisioner.RotateUserCredentials(ctx, id)
		if err != nil {
			response.RespondInternalError(w, r, fmt.Sprintf("Failed to rotate credentials: %v", err))
			return
		}
		token = updated.SubscriptionToken.String()
	} else {
		updated, err := h.repo.RotateSubscriptionToken(ctx, id)
		if err != nil {
			response.RespondNotFound(w, r, "User not found")
			return
		}
		token = updated.SubscriptionToken.String()
	}

	subURL := fmt.Sprintf("/sub/%s", token)

	if h.audit != nil {
		_ = h.audit.Log(r, "rotate_keys", "user", &id, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(SubscriptionResponse{
		SubscriptionToken: token,
		SubscriptionURL:   subURL,
	})
}

func (h *UserHandler) GetByTelegramID(w http.ResponseWriter, r *http.Request) {
	tgIDStr := chi.URLParam(r, "tg_id")
	var tgID int64
	if _, err := fmt.Sscanf(tgIDStr, "%d", &tgID); err != nil || tgID <= 0 {
		response.RespondBadRequest(w, r, "Invalid telegram ID", nil)
		return
	}

	user, err := h.repo.GetByTelegramID(r.Context(), tgID)
	if err != nil {
		response.RespondNotFound(w, r, "User with given Telegram ID not found")
		return
	}

	token := user.SubscriptionToken.String()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"user":               user,
		"subscription_token": token,
		"subscription_url":   fmt.Sprintf("/sub/%s", token),
	})
}

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
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type UserHandler struct {
	repo     store.UserRepository
	planRepo store.PlanRepository
	audit    *middleware.AuditService
}

func NewUserHandler(repo store.UserRepository, planRepo store.PlanRepository, audit *middleware.AuditService) *UserHandler {
	return &UserHandler{
		repo:     repo,
		planRepo: planRepo,
		audit:    audit,
	}
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
		TrafficUsed:  existing.TrafficUsed,
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

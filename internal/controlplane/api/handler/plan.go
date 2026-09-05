package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type PlanHandler struct {
	repo  store.PlanRepository
	audit *middleware.AuditService
}

func NewPlanHandler(repo store.PlanRepository, audit *middleware.AuditService) *PlanHandler {
	return &PlanHandler{
		repo:  repo,
		audit: audit,
	}
}

type CreatePlanRequest struct {
	Name               string   `json:"name" validate:"required,min=2,max=128"`
	Price              string   `json:"price" validate:"required"`
	DeviceLimit        int32    `json:"device_limit" validate:"min=1,max=100"`
	TrafficLimit       *int64   `json:"traffic_limit,omitempty" validate:"omitempty,min=0"`
	Protocols          []string `json:"protocols" validate:"required,min=1"`
	IsTrial            bool     `json:"is_trial,omitempty"`
	TrialDurationHours int32    `json:"trial_duration_hours,omitempty"`
	PriceStars         int32    `json:"price_stars,omitempty"`
}

type UpdatePlanRequest struct {
	Name               string   `json:"name" validate:"omitempty,min=2,max=128"`
	Price              string   `json:"price,omitempty"`
	DeviceLimit        *int32   `json:"device_limit,omitempty" validate:"omitempty,min=1,max=100"`
	TrafficLimit       *int64   `json:"traffic_limit,omitempty" validate:"omitempty,min=0"`
	Protocols          []string `json:"protocols,omitempty" validate:"omitempty,min=1"`
	IsActive           *bool    `json:"is_active,omitempty"`
	IsTrial            *bool    `json:"is_trial,omitempty"`
	TrialDurationHours *int32   `json:"trial_duration_hours,omitempty"`
	PriceStars         *int32   `json:"price_stars,omitempty"`
}

func (h *PlanHandler) List(w http.ResponseWriter, r *http.Request) {
	plans, err := h.repo.List(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve plans")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(plans)
}

func (h *PlanHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreatePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	if req.DeviceLimit <= 0 {
		req.DeviceLimit = 1
	}

	var priceNumeric pgtype.Numeric
	if err := priceNumeric.Scan(req.Price); err != nil {
		response.RespondBadRequest(w, r, "Invalid price format", map[string]string{"price": "must be a valid decimal number"})
		return
	}

	var trLimit pgtype.Int8
	if req.TrafficLimit != nil {
		trLimit = pgtype.Int8{Int64: *req.TrafficLimit, Valid: true}
	}

	trialHours := req.TrialDurationHours
	if req.IsTrial && trialHours <= 0 {
		trialHours = 24
	}

	plan, err := h.repo.Create(r.Context(), store.CreatePlanParams{
		Name:               strings.TrimSpace(req.Name),
		MonthlyPrice:       priceNumeric,
		TrafficLimit:       trLimit,
		DeviceLimit:        pgtype.Int4{Int32: req.DeviceLimit, Valid: true},
		Protocols:          req.Protocols,
		Features:           []byte("{}"),
		IsActive:           pgtype.Bool{Bool: true, Valid: true},
		IsTrial:            pgtype.Bool{Bool: req.IsTrial, Valid: true},
		TrialDurationHours: pgtype.Int4{Int32: trialHours, Valid: trialHours > 0},
		PriceStars:         pgtype.Int4{Int32: req.PriceStars, Valid: req.PriceStars > 0},
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to create plan")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "create", "plan", &plan.ID, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(plan)
}

func (h *PlanHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid plan ID", nil)
		return
	}

	plan, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "Plan not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(plan)
}

func (h *PlanHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid plan ID", nil)
		return
	}

	var req UpdatePlanRequest
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
		response.RespondNotFound(w, r, "Plan not found")
		return
	}

	name := existing.Name
	if req.Name != "" {
		name = strings.TrimSpace(req.Name)
	}
	price := existing.MonthlyPrice
	if req.Price != "" {
		var newPrice pgtype.Numeric
		if err := newPrice.Scan(req.Price); err == nil {
			price = newPrice
		}
	}
	deviceLimit := existing.DeviceLimit
	if req.DeviceLimit != nil {
		deviceLimit = pgtype.Int4{Int32: *req.DeviceLimit, Valid: true}
	}
	trLimit := existing.TrafficLimit
	if req.TrafficLimit != nil {
		trLimit = pgtype.Int8{Int64: *req.TrafficLimit, Valid: true}
	}
	protocols := existing.Protocols
	if len(req.Protocols) > 0 {
		protocols = req.Protocols
	}
	isActive := existing.IsActive
	if req.IsActive != nil {
		isActive = pgtype.Bool{Bool: *req.IsActive, Valid: true}
	}
	isTrial := existing.IsTrial
	if req.IsTrial != nil {
		isTrial = pgtype.Bool{Bool: *req.IsTrial, Valid: true}
	}
	trialHours := existing.TrialDurationHours
	if req.TrialDurationHours != nil {
		trialHours = pgtype.Int4{Int32: *req.TrialDurationHours, Valid: *req.TrialDurationHours > 0}
	}
	priceStars := existing.PriceStars
	if req.PriceStars != nil {
		priceStars = pgtype.Int4{Int32: *req.PriceStars, Valid: *req.PriceStars > 0}
	}

	updated, err := h.repo.Update(r.Context(), store.UpdatePlanParams{
		ID:                 id,
		Name:               name,
		MonthlyPrice:       price,
		TrafficLimit:       trLimit,
		DeviceLimit:        deviceLimit,
		Protocols:          protocols,
		Features:           existing.Features,
		IsActive:           isActive,
		IsTrial:            isTrial,
		TrialDurationHours: trialHours,
		PriceStars:         priceStars,
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to update plan")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "update", "plan", &id, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *PlanHandler) GetTrial(w http.ResponseWriter, r *http.Request) {
	plan, err := h.repo.GetTrial(r.Context())
	if err != nil {
		response.RespondNotFound(w, r, "No active trial plan found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(plan)
}

func (h *PlanHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid plan ID", nil)
		return
	}

	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		response.RespondNotFound(w, r, "Plan not found")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		response.RespondInternalError(w, r, "Failed to delete plan")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "delete", "plan", &id, nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

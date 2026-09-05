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
	Price              string   `json:"price,omitempty"`
	Price1m            string   `json:"price_1m,omitempty"`
	Price3m            string   `json:"price_3m,omitempty"`
	Price6m            string   `json:"price_6m,omitempty"`
	Price12m           string   `json:"price_12m,omitempty"`
	DeviceLimit        int32    `json:"device_limit,omitempty" validate:"omitempty,min=1,max=100"`
	MaxDevices         int32    `json:"max_devices,omitempty" validate:"omitempty,min=1,max=100"`
	TrafficLimit       *int64   `json:"traffic_limit,omitempty" validate:"omitempty,min=0"`
	TrafficLimitGB     int64    `json:"traffic_limit_gb,omitempty" validate:"omitempty,min=0"`
	Protocols          []string `json:"protocols" validate:"required,min=1"`
	IsTrial            bool     `json:"is_trial,omitempty"`
	TrialDurationHours int32    `json:"trial_duration_hours,omitempty"`
	PriceStars         int32    `json:"price_stars,omitempty"`
}

type UpdatePlanRequest struct {
	Name               string   `json:"name" validate:"omitempty,min=2,max=128"`
	Price              string   `json:"price,omitempty"`
	Price1m            string   `json:"price_1m,omitempty"`
	Price3m            string   `json:"price_3m,omitempty"`
	Price6m            string   `json:"price_6m,omitempty"`
	Price12m           string   `json:"price_12m,omitempty"`
	DeviceLimit        *int32   `json:"device_limit,omitempty" validate:"omitempty,min=1,max=100"`
	MaxDevices         *int32   `json:"max_devices,omitempty" validate:"omitempty,min=1,max=100"`
	TrafficLimit       *int64   `json:"traffic_limit,omitempty" validate:"omitempty,min=0"`
	TrafficLimitGB     *int64   `json:"traffic_limit_gb,omitempty" validate:"omitempty,min=0"`
	Protocols          []string `json:"protocols,omitempty" validate:"omitempty,min=1"`
	IsActive           *bool    `json:"is_active,omitempty"`
	IsTrial            *bool    `json:"is_trial,omitempty"`
	TrialDurationHours *int32   `json:"trial_duration_hours,omitempty"`
	PriceStars         *int32   `json:"price_stars,omitempty"`
}

func validateProtocols(protocols []string) bool {
	if len(protocols) == 0 {
		return false
	}
	allowed := map[string]bool{
		"wireguard": true,
		"amneziawg": true,
		"vless":     true,
	}
	for _, p := range protocols {
		if !allowed[strings.ToLower(strings.TrimSpace(p))] {
			return false
		}
	}
	return true
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

	if !validateProtocols(req.Protocols) {
		response.RespondBadRequest(w, r, "Invalid protocol: allowed protocols are wireguard, amneziawg, vless", map[string]string{
			"protocols": "must contain only valid protocols (wireguard, amneziawg, vless)",
		})
		return
	}

	devLimit := req.MaxDevices
	if devLimit <= 0 {
		devLimit = req.DeviceLimit
	}
	if devLimit <= 0 {
		devLimit = 3
	}

	price1mStr := req.Price1m
	if price1mStr == "" {
		price1mStr = req.Price
	}
	if price1mStr == "" {
		price1mStr = "0"
	}

	var priceNumeric, price1mNum, price3mNum, price6mNum, price12mNum pgtype.Numeric
	if err := priceNumeric.Scan(price1mStr); err != nil {
		response.RespondBadRequest(w, r, "Invalid price format", map[string]string{"price": "must be a valid decimal number"})
		return
	}
	_ = price1mNum.Scan(price1mStr)

	if req.Price3m != "" {
		_ = price3mNum.Scan(req.Price3m)
	}
	if req.Price6m != "" {
		_ = price6mNum.Scan(req.Price6m)
	}
	if req.Price12m != "" {
		_ = price12mNum.Scan(req.Price12m)
	}

	var trLimit pgtype.Int8
	var trLimitGB pgtype.Int4
	if req.TrafficLimitGB > 0 {
		trLimitGB = pgtype.Int4{Int32: int32(req.TrafficLimitGB), Valid: true}
		trLimit = pgtype.Int8{Int64: req.TrafficLimitGB * 1024 * 1024 * 1024, Valid: true}
	} else if req.TrafficLimit != nil {
		trLimit = pgtype.Int8{Int64: *req.TrafficLimit, Valid: true}
		trLimitGB = pgtype.Int4{Int32: int32(*req.TrafficLimit / (1024 * 1024 * 1024)), Valid: true}
	}

	trialHours := req.TrialDurationHours
	if req.IsTrial && trialHours <= 0 {
		trialHours = 24
	}

	cleanProtocols := make([]string, 0, len(req.Protocols))
	for _, p := range req.Protocols {
		cleanProtocols = append(cleanProtocols, strings.ToLower(strings.TrimSpace(p)))
	}

	plan, err := h.repo.Create(r.Context(), store.CreatePlanParams{
		Name:               strings.TrimSpace(req.Name),
		MonthlyPrice:       priceNumeric,
		TrafficLimit:       trLimit,
		DeviceLimit:        pgtype.Int4{Int32: devLimit, Valid: true},
		Protocols:          cleanProtocols,
		Features:           []byte("{}"),
		IsActive:           pgtype.Bool{Bool: true, Valid: true},
		IsTrial:            pgtype.Bool{Bool: req.IsTrial, Valid: true},
		TrialDurationHours: pgtype.Int4{Int32: trialHours, Valid: trialHours > 0},
		PriceStars:         pgtype.Int4{Int32: req.PriceStars, Valid: req.PriceStars > 0},
		MaxDevices:         pgtype.Int4{Int32: devLimit, Valid: true},
		TrafficLimitGb:     trLimitGB,
		Price1m:            price1mNum,
		Price3m:            price3mNum,
		Price6m:            price6mNum,
		Price12m:           price12mNum,
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
	} else if req.Price1m != "" {
		var newPrice pgtype.Numeric
		if err := newPrice.Scan(req.Price1m); err == nil {
			price = newPrice
		}
	}

	price1m := existing.Price1m
	if req.Price1m != "" {
		_ = price1m.Scan(req.Price1m)
	} else if req.Price != "" {
		_ = price1m.Scan(req.Price)
	}

	price3m := existing.Price3m
	if req.Price3m != "" {
		_ = price3m.Scan(req.Price3m)
	}

	price6m := existing.Price6m
	if req.Price6m != "" {
		_ = price6m.Scan(req.Price6m)
	}

	price12m := existing.Price12m
	if req.Price12m != "" {
		_ = price12m.Scan(req.Price12m)
	}

	deviceLimit := existing.DeviceLimit
	if req.MaxDevices != nil && *req.MaxDevices > 0 {
		deviceLimit = pgtype.Int4{Int32: *req.MaxDevices, Valid: true}
	} else if req.DeviceLimit != nil && *req.DeviceLimit > 0 {
		deviceLimit = pgtype.Int4{Int32: *req.DeviceLimit, Valid: true}
	}

	maxDevices := deviceLimit

	trLimit := existing.TrafficLimit
	trLimitGB := existing.TrafficLimitGb
	if req.TrafficLimitGB != nil && *req.TrafficLimitGB > 0 {
		trLimitGB = pgtype.Int4{Int32: int32(*req.TrafficLimitGB), Valid: true}
		trLimit = pgtype.Int8{Int64: *req.TrafficLimitGB * 1024 * 1024 * 1024, Valid: true}
	} else if req.TrafficLimit != nil {
		trLimit = pgtype.Int8{Int64: *req.TrafficLimit, Valid: true}
		trLimitGB = pgtype.Int4{Int32: int32(*req.TrafficLimit / (1024 * 1024 * 1024)), Valid: true}
	}

	protocols := existing.Protocols
	if len(req.Protocols) > 0 {
		if !validateProtocols(req.Protocols) {
			response.RespondBadRequest(w, r, "Invalid protocol: allowed protocols are wireguard, amneziawg, vless", map[string]string{
				"protocols": "must contain only valid protocols (wireguard, amneziawg, vless)",
			})
			return
		}
		cleanProtocols := make([]string, 0, len(req.Protocols))
		for _, p := range req.Protocols {
			cleanProtocols = append(cleanProtocols, strings.ToLower(strings.TrimSpace(p)))
		}
		protocols = cleanProtocols
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
		MaxDevices:         maxDevices,
		TrafficLimitGb:     trLimitGB,
		Price1m:            price1m,
		Price3m:            price3m,
		Price6m:            price6m,
		Price12m:           price12m,
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

package web

import (
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) Plans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "plans")
	plans, _ := h.repos.Plans.List(ctx)
	data["Plans"] = plans
	_ = h.tmpl.Render(w, "plans.html", data)
}

// POST /admin/plans

func (h *Handler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManagePlans {
		http.Redirect(w, r, "/admin/plans?error=Forbidden:+permission+to+manage+plans+is+required", http.StatusSeeOther)
		return
	}

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

	var price1mStr string
	if isTrial {
		price1mStr = "0"
		priceStars = 0
	} else {
		price1mStr = r.FormValue("price_1m")
		if price1mStr == "" {
			price1mStr = r.FormValue("price")
		}
		if price1mStr == "" {
			price1mStr = "0"
		}
	}

	var priceNumeric, price1mNum, price3mNum, price6mNum, price12mNum pgtype.Numeric
	_ = priceNumeric.Scan(price1mStr)
	_ = price1mNum.Scan(price1mStr)

	if !isTrial {
		if p3 := r.FormValue("price_3m"); p3 != "" {
			_ = price3mNum.Scan(p3)
		}
		if p6 := r.FormValue("price_6m"); p6 != "" {
			_ = price6mNum.Scan(p6)
		}
		if p12 := r.FormValue("price_12m"); p12 != "" {
			_ = price12mNum.Scan(p12)
		}
	}

	createdPlan, _ := h.repos.Plans.Create(r.Context(), store.CreatePlanParams{
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

	h.recordAudit(r, "CreatePlan", "plan", &createdPlan.ID, fmt.Sprintf("Service plan %s created", name))

	http.Redirect(w, r, "/admin/plans?success=Plan+created+successfully", http.StatusSeeOther)
}

// POST /admin/plans/{id}

func (h *Handler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManagePlans {
		http.Redirect(w, r, "/admin/plans?error=Forbidden:+permission+to+manage+plans+is+required", http.StatusSeeOther)
		return
	}

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

	var priceNumeric, price1mNum, price3mNum, price6mNum, price12mNum pgtype.Numeric
	if isTrial {
		_ = priceNumeric.Scan("0")
		_ = price1mNum.Scan("0")
		priceStars = 0
	} else {
		price1mStr := r.FormValue("price_1m")
		if price1mStr == "" {
			price1mStr = r.FormValue("price")
		}

		priceNumeric = existing.MonthlyPrice
		price1mNum = existing.Price1m
		if price1mStr != "" {
			_ = priceNumeric.Scan(price1mStr)
			_ = price1mNum.Scan(price1mStr)
		}

		price3mNum = existing.Price3m
		if p3 := r.FormValue("price_3m"); p3 != "" {
			_ = price3mNum.Scan(p3)
		}

		price6mNum = existing.Price6m
		if p6 := r.FormValue("price_6m"); p6 != "" {
			_ = price6mNum.Scan(p6)
		}

		price12mNum = existing.Price12m
		if p12 := r.FormValue("price_12m"); p12 != "" {
			_ = price12mNum.Scan(p12)
		}
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
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManagePlans {
		http.Redirect(w, r, "/admin/plans?error=Forbidden:+permission+to+manage+plans+is+required", http.StatusSeeOther)
		return
	}

	planIDStr := chi.URLParam(r, "id")
	if planID, err := uuid.Parse(planIDStr); err == nil {
		targetPlan, _ := h.repos.Plans.GetByID(r.Context(), planID)
		_ = h.repos.Plans.Delete(r.Context(), planID)
		h.recordAudit(r, "DeletePlan", "plan", &planID, fmt.Sprintf("Service plan %s deleted", targetPlan.Name))
	}
	http.Redirect(w, r, "/admin/plans?success=Plan+deleted+successfully", http.StatusSeeOther)
}

// GET /admin/credentials

package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
)

type AnalyticsHandler struct {
	trafficRepo store.TrafficRepository
}

func NewAnalyticsHandler(trafficRepo store.TrafficRepository) *AnalyticsHandler {
	return &AnalyticsHandler{
		trafficRepo: trafficRepo,
	}
}

type TrafficOverviewResponse struct {
	TotalRxBytes int64 `json:"total_rx_bytes"`
	TotalTxBytes int64 `json:"total_tx_bytes"`
	From         string `json:"from"`
	To           string `json:"to"`
}

func (h *AnalyticsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	nodeStats, err := h.trafficRepo.GetAggregateByNode(r.Context(), from, to)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve traffic overview")
		return
	}

	var totalRx, totalTx int64
	for _, s := range nodeStats {
		totalRx += s.TotalRx
		totalTx += s.TotalTx
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(TrafficOverviewResponse{
		TotalRxBytes: totalRx,
		TotalTxBytes: totalTx,
		From:         from.Format(time.RFC3339),
		To:           to.Format(time.RFC3339),
	})
}

func (h *AnalyticsHandler) GetByUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid user ID", nil)
		return
	}

	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	stats, err := h.trafficRepo.ListByUser(r.Context(), userID, from, to)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve user traffic series")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}

func (h *AnalyticsHandler) GetByNode(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	stats, err := h.trafficRepo.GetAggregateByNode(r.Context(), from, to)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve node traffic series")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}

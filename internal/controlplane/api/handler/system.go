package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/web"
)

type SystemHandler struct{}

func NewSystemHandler() *SystemHandler {
	return &SystemHandler{}
}

type SystemTelemetryResponse struct {
	CPUPercent float64              `json:"cpu_percent"`
	CPUModel   string               `json:"cpu_model"`
	RAMPercent float64              `json:"ram_percent"`
	RAMUsed    int64                `json:"ram_used"`
	RAMTotal   int64                `json:"ram_total"`
	DiskPercent float64             `json:"disk_percent"`
	DiskUsed   int64                `json:"disk_used"`
	DiskTotal  int64                `json:"disk_total"`
	RxSpeed    int64                `json:"rx_speed"`
	TxSpeed    int64                `json:"tx_speed"`
	History    []web.MetricSnapshot `json:"history"`
}

func (h *SystemHandler) GetTelemetry(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if val, err := strconv.Atoi(lStr); err == nil && val > 0 && val <= 300 {
			limit = val
		}
	}

	step := 3
	if sStr := r.URL.Query().Get("step"); sStr != "" {
		if val, err := strconv.Atoi(sStr); err == nil && val > 0 && val <= 300 {
			step = val
		}
	}

	cpuPercent, cpuModel, ramUsed, ramTotal, diskUsed, diskTotal := web.ReadHostTelemetry()

	ramPercent := 0.0
	if ramTotal > 0 {
		ramPercent = (float64(ramUsed) / float64(ramTotal)) * 100.0
	}

	diskPercent := 0.0
	if diskTotal > 0 {
		diskPercent = (float64(diskUsed) / float64(diskTotal)) * 100.0
	}

	history := web.GlobalTelemetryHistory.GetSampledHistory(step, limit)

	rxRate, txRate := web.ReadHostNetworkRates()

	resp := SystemTelemetryResponse{
		CPUPercent:  cpuPercent,
		CPUModel:    cpuModel,
		RAMPercent:  ramPercent,
		RAMUsed:     ramUsed,
		RAMTotal:    ramTotal,
		DiskPercent: diskPercent,
		DiskUsed:    diskUsed,
		DiskTotal:   diskTotal,
		RxSpeed:     rxRate,
		TxSpeed:     txRate,
		History:     history,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

package web

import (
	"sync"
	"time"
)

// MetricSnapshot records an instantaneous real metric probe.
type MetricSnapshot struct {
	Timestamp  time.Time `json:"timestamp"`
	CPUPercent float64   `json:"cpu_percent"`
	RAMPercent float64   `json:"ram_percent"`
	DiskPercent float64  `json:"disk_percent"`
	RxSpeed    int64     `json:"rx_speed"`
	TxSpeed    int64     `json:"tx_speed"`
}

// TelemetryHistoryRing keeps a bounded in-memory buffer of actual chronological snapshots.
type TelemetryHistoryRing struct {
	mu        sync.RWMutex
	snapshots []MetricSnapshot
	maxSize   int
}

var GlobalTelemetryHistory = NewTelemetryHistoryRing(300) // Keep up to 300 snapshots

func NewTelemetryHistoryRing(maxSize int) *TelemetryHistoryRing {
	r := &TelemetryHistoryRing{
		snapshots: make([]MetricSnapshot, 0, maxSize),
		maxSize:   maxSize,
	}
	// Start collector ticker every 1 second
	go r.startCollector()
	return r
}

func (r *TelemetryHistoryRing) startCollector() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cpu, _, ramUsed, ramTotal, diskUsed, diskTotal := ReadHostTelemetry()
		rxRate, txRate := ReadHostNetworkRates()
		ramPct := 0.0
		if ramTotal > 0 {
			ramPct = (float64(ramUsed) / float64(ramTotal)) * 100.0
		}
		diskPct := 0.0
		if diskTotal > 0 {
			diskPct = (float64(diskUsed) / float64(diskTotal)) * 100.0
		}

		snap := MetricSnapshot{
			Timestamp:   time.Now().UTC(),
			CPUPercent:  cpu,
			RAMPercent:  ramPct,
			DiskPercent: diskPct,
			RxSpeed:     rxRate,
			TxSpeed:     txRate,
		}

		r.mu.Lock()
		if len(r.snapshots) >= r.maxSize {
			r.snapshots = r.snapshots[1:]
		}
		r.snapshots = append(r.snapshots, snap)
		r.mu.Unlock()
	}
}

// GetHistory returns recent real snapshots matching count.
func (r *TelemetryHistoryRing) GetHistory(limit int) []MetricSnapshot {
	return r.GetSampledHistory(1, limit)
}

// GetSampledHistory returns chronological snapshots sampled every `step` seconds, up to `limit` points.
func (r *TelemetryHistoryRing) GetSampledHistory(stepSec int, limit int) []MetricSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	n := len(r.snapshots)
	if n == 0 {
		return nil
	}
	if stepSec <= 0 {
		stepSec = 1
	}
	if limit <= 0 {
		limit = 30
	}

	// Step backwards from the latest snapshot by stepSec
	var picked []MetricSnapshot
	for i := n - 1; i >= 0 && len(picked) < limit; i -= stepSec {
		picked = append(picked, r.snapshots[i])
	}

	// Reverse picked to restore chronological order (oldest to newest)
	total := len(picked)
	res := make([]MetricSnapshot, total)
	for i := 0; i < total; i++ {
		res[i] = picked[total-1-i]
	}
	return res
}

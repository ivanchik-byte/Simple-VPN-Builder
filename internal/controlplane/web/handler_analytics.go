package web

import (
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"time"
)

func (h *Handler) Analytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "analytics")

	now := time.Now()
	from := now.Add(-30 * 24 * time.Hour)
	nodeStats, _ := h.repos.Traffic.GetAggregateByNode(ctx, from, now)
	auditLogs, _ := h.repos.AuditLogs.List(ctx, store.ListAuditLogsParams{
		Column1:     uuid.Nil,
		Column2:     "",
		Column3:     "",
		CreatedAt:   pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
		CreatedAt_2: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		Limit:       50,
		Offset:      0,
	})

	var totalRx, totalTx int64
	for _, ns := range nodeStats {
		totalRx += ns.TotalRx
		totalTx += ns.TotalTx
	}

	data["Analytics"] = map[string]int64{
		"TotalRxBytes": totalRx,
		"TotalTxBytes": totalTx,
		"TotalBytes":   totalRx + totalTx,
	}
	data["AuditLogs"] = auditLogs

	_ = h.tmpl.Render(w, "analytics.html", data)
}

// GET /admin/settings

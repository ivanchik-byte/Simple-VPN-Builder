package web

import (
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"net/url"
)

func (h *Handler) Nodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := h.basePageData(r, "nodes")
	nodes, _, _ := h.repos.Nodes.List(ctx, store.NodeFilter{Limit: 100, Offset: 0})
	data["Nodes"] = nodes
	_ = h.tmpl.Render(w, "nodes.html", data)
}

// POST /admin/nodes

func (h *Handler) CreateNode(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageNodes {
		http.Redirect(w, r, "/admin/nodes?error=Forbidden:+permission+to+manage+nodes+is+required", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
		return
	}

	host := r.FormValue("host")
	port := r.FormValue("port")
	if port == "" {
		port = "443"
	}
	endpoint := host + ":" + port

	_, _ = h.repos.Nodes.Create(r.Context(), store.CreateNodeParams{
		Name:         r.FormValue("name"),
		Endpoint:     endpoint,
		GrpcEndpoint: host + ":9090",
		Region:       pgtype.Text{String: r.FormValue("region"), Valid: true},
		PublicKey:    r.FormValue("public_key"),
		Status:       pgtype.Text{String: "offline", Valid: true},
	})

	http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
}

// GET /admin/nodes/{id}

func (h *Handler) NodeDetail(w http.ResponseWriter, r *http.Request) {
	nodeIDStr := chi.URLParam(r, "id")
	nodeID, err := uuid.Parse(nodeIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	node, err := h.repos.Nodes.GetByID(ctx, nodeID)
	if err != nil {
		http.Redirect(w, r, "/admin/nodes", http.StatusSeeOther)
		return
	}

	data := h.basePageData(r, "nodes")
	data["Node"] = node

	var telemetry *TelemetryData
	if h.sessionMgr != nil {
		if session, ok := h.sessionMgr.Get(nodeID); ok && session != nil {
			if sys := session.GetSystemInfo(); sys != nil {
				ramPercent := 0.0
				if sys.MemoryTotal > 0 {
					ramPercent = (float64(sys.MemoryUsed) / float64(sys.MemoryTotal)) * 100.0
				}
				diskPercent := 0.0
				if sys.DiskTotal > 0 {
					diskPercent = (float64(sys.DiskUsed) / float64(sys.DiskTotal)) * 100.0
				}
				var rxSpeed, txSpeed int64
				for _, iface := range sys.Networks {
					rxSpeed += int64(iface.RxBytes)
					txSpeed += int64(iface.TxBytes)
				}
				telemetry = &TelemetryData{
					CPUPercent:  sys.CpuUsagePercent,
					RAMPercent:  ramPercent,
					RAMUsed:     int64(sys.MemoryUsed),
					RAMTotal:    int64(sys.MemoryTotal),
					DiskPercent: diskPercent,
					DiskUsed:    int64(sys.DiskUsed),
					DiskTotal:   int64(sys.DiskTotal),
					RxSpeed:     rxSpeed,
					TxSpeed:     txSpeed,
				}
			}
		}
	}
	data["Telemetry"] = telemetry

	creds, _ := h.repos.Credentials.ListActiveByNode(ctx, nodeID)
	data["Credentials"] = creds

	_ = h.tmpl.Render(w, "node_detail.html", data)
}

// POST /admin/nodes/{id}/status

func (h *Handler) UpdateNodeStatus(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageNodes {
		http.Redirect(w, r, "/admin/nodes?error=Forbidden:+permission+to+manage+nodes+is+required", http.StatusSeeOther)
		return
	}

	nodeIDStr := chi.URLParam(r, "id")
	nodeID, err := uuid.Parse(nodeIDStr)
	if err == nil {
		status := r.FormValue("status")
		_ = h.repos.Nodes.UpdateHeartbeat(r.Context(), nodeID, status)
	}
	http.Redirect(w, r, "/admin/nodes/"+nodeIDStr, http.StatusSeeOther)
}

// POST /admin/nodes/{id}/delete

func (h *Handler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	perms := h.getCallerPermissions(r.Context())
	if !perms.CanManageNodes {
		http.Redirect(w, r, "/admin/nodes?error=Forbidden:+permission+to+manage+nodes+is+required", http.StatusSeeOther)
		return
	}
	adminCtx := GetAdminContext(r.Context())

	nodeIDStr := chi.URLParam(r, "id")
	nodeID, err := uuid.Parse(nodeIDStr)
	if err != nil {
		http.Redirect(w, r, "/admin/nodes?error=Invalid+node+ID", http.StatusSeeOther)
		return
	}

	targetNode, _ := h.repos.Nodes.GetByID(r.Context(), nodeID)
	nodeName := "unknown"
	if targetNode.Name != "" {
		nodeName = targetNode.Name
	}

	if err := h.repos.Nodes.Delete(r.Context(), nodeID); err != nil {
		http.Redirect(w, r, "/admin/nodes?error=Failed+to+delete+node:+"+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.recordAudit(r, "DeleteNode", "node", &nodeID, fmt.Sprintf("Node %s (%s) deleted", nodeName, nodeID.String()[:8]))

	if h.alertDispatcher != nil {
		h.alertDispatcher.SendInfraAlert(nodeName, "deleted", fmt.Sprintf("Deleted by %s", adminCtx.Username))
	}

	http.Redirect(w, r, "/admin/nodes?success=Node+deleted+successfully", http.StatusSeeOther)
}

// GET /admin/users

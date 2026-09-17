package web

import (
	"github.com/go-chi/chi/v5"
	"fmt"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"net/url"
)

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

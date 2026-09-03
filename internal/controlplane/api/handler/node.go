package handler

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type NodeHandler struct {
	repo  store.NodeRepository
	audit *middleware.AuditService
}

func NewNodeHandler(repo store.NodeRepository, audit *middleware.AuditService) *NodeHandler {
	return &NodeHandler{
		repo:  repo,
		audit: audit,
	}
}

type CreateNodeRequest struct {
	Name      string   `json:"name" validate:"required,min=2,max=128"`
	Region    string   `json:"region" validate:"required,min=2,max=64"`
	Country   string   `json:"country" validate:"required,min=2,max=64"`
	City      string   `json:"city" validate:"required,min=2,max=64"`
	PublicIP  string   `json:"public_ip" validate:"required,ip"`
	Capacity  int32    `json:"capacity" validate:"required,min=1,max=100000"`
	Protocols []string `json:"protocols" validate:"required,min=1"`
	Tags      []string `json:"tags,omitempty"`
}

type UpdateNodeRequest struct {
	Name      string   `json:"name" validate:"omitempty,min=2,max=128"`
	Status    string   `json:"status" validate:"omitempty,oneof=active maintenance offline drain"`
	Capacity  int32    `json:"capacity" validate:"omitempty,min=1,max=100000"`
	Protocols []string `json:"protocols" validate:"omitempty,min=1"`
	Tags      []string `json:"tags,omitempty"`
}

func (h *NodeHandler) List(w http.ResponseWriter, r *http.Request) {
	pagination := request.ParsePagination(r)
	status := r.URL.Query().Get("status")
	region := r.URL.Query().Get("region")

	filter := store.NodeFilter{
		Status: status,
		Region: region,
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	}

	nodes, total, err := h.repo.List(r.Context(), filter)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve nodes")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(request.NewPaginatedResponse(nodes, pagination, total))
}

func (h *NodeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	trimmedIP := strings.TrimSpace(req.PublicIP)
	if _, err := netip.ParseAddr(trimmedIP); err != nil {
		response.RespondBadRequest(w, r, "Invalid public IP address", map[string]string{"public_ip": "invalid IP format"})
		return
	}

	endpoint := trimmedIP + ":51820"
	grpcEndpoint := trimmedIP + ":9090"
	tagsBytes, _ := json.Marshal(req.Tags)

	node, err := h.repo.Create(r.Context(), store.CreateNodeParams{
		Name:         req.Name,
		Endpoint:     endpoint,
		GrpcEndpoint: grpcEndpoint,
		Region:       pgtype.Text{String: req.Region, Valid: req.Region != ""},
		CapacityGbps: pgtype.Int4{Int32: req.Capacity, Valid: true},
		Status:       pgtype.Text{String: "active", Valid: true},
		Tags:         tagsBytes,
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to create node")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "create", "node", &node.ID, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(node)
}

func (h *NodeHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid node ID", nil)
		return
	}

	node, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "Node not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(node)
}

func (h *NodeHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid node ID", nil)
		return
	}

	var req UpdateNodeRequest
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
		response.RespondNotFound(w, r, "Node not found")
		return
	}

	name := existing.Name
	if req.Name != "" {
		name = req.Name
	}
	status := existing.Status
	if req.Status != "" {
		status = pgtype.Text{String: req.Status, Valid: true}
	}
	capacity := existing.CapacityGbps
	if req.Capacity > 0 {
		capacity = pgtype.Int4{Int32: req.Capacity, Valid: true}
	}
	tags := existing.Tags
	if req.Tags != nil {
		tags, _ = json.Marshal(req.Tags)
	}

	updated, err := h.repo.Update(r.Context(), store.UpdateNodeParams{
		ID:              id,
		Name:            name,
		Endpoint:        existing.Endpoint,
		GrpcEndpoint:    existing.GrpcEndpoint,
		Region:          existing.Region,
		CapacityGbps:    capacity,
		Status:          status,
		Tags:            tags,
		PublicKey:       existing.PublicKey,
		CertFingerprint: existing.CertFingerprint,
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to update node")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "update", "node", &id, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *NodeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid node ID", nil)
		return
	}

	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		response.RespondNotFound(w, r, "Node not found")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		response.RespondInternalError(w, r, "Failed to delete node")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "delete", "node", &id, nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

type NodeStatsResponse struct {
	NodeID      uuid.UUID          `json:"node_id"`
	Status      string             `json:"status"`
	Capacity    int32              `json:"capacity"`
	LastHeartAt pgtype.Timestamptz `json:"last_heartbeat_at"`
}

func (h *NodeHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid node ID", nil)
		return
	}

	node, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "Node not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(NodeStatsResponse{
		NodeID:      node.ID,
		Status:      node.Status.String,
		Capacity:    node.CapacityGbps.Int32,
		LastHeartAt: node.LastHeartbeat,
	})
}

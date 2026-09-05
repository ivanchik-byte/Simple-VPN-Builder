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
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type CredentialHandler struct {
	repo     store.CredentialRepository
	userRepo store.UserRepository
	nodeRepo store.NodeRepository
	audit    *middleware.AuditService
}

func NewCredentialHandler(
	repo store.CredentialRepository,
	userRepo store.UserRepository,
	nodeRepo store.NodeRepository,
	audit *middleware.AuditService,
) *CredentialHandler {
	return &CredentialHandler{
		repo:     repo,
		userRepo: userRepo,
		nodeRepo: nodeRepo,
		audit:    audit,
	}
}

type CreateCredentialRequest struct {
	UserID    uuid.UUID `json:"user_id" validate:"required,uuid"`
	NodeID    uuid.UUID `json:"node_id" validate:"required,uuid"`
	Protocol  string    `json:"protocol" validate:"required,oneof=wireguard amneziawg vless shadowsocks"`
	PublicKey string    `json:"public_key,omitempty"`
	IPv4      string    `json:"ipv4,omitempty" validate:"omitempty,ip"`
	IPv6      string    `json:"ipv6,omitempty" validate:"omitempty,ip"`
	// AmneziaWG specific obfuscation parameters
	AwgJc   *int32 `json:"awg_jc,omitempty" validate:"omitempty,min=1,max=128"`
	AwgJmin *int32 `json:"awg_jmin,omitempty" validate:"omitempty,min=0,max=1280"`
	AwgJmax *int32 `json:"awg_jmax,omitempty" validate:"omitempty,min=0,max=1280"`
	AwgS1   *int32 `json:"awg_s1,omitempty" validate:"omitempty,min=15,max=1280"`
	AwgS2   *int32 `json:"awg_s2,omitempty" validate:"omitempty,min=15,max=1280"`
	AwgH1   *int64 `json:"awg_h1,omitempty"`
	AwgH2   *int64 `json:"awg_h2,omitempty"`
	AwgH3   *int64  `json:"awg_h3,omitempty"`
	AwgH4   *int64  `json:"awg_h4,omitempty"`
	Uuid    *string `json:"uuid,omitempty"`
	Flow    *string `json:"flow,omitempty"`
}

type ProvisionCredentialResponse struct {
	Credential store.Credential `json:"credential"`
	PrivateKey string           `json:"private_key,omitempty"`
}

func (h *CredentialHandler) List(w http.ResponseWriter, r *http.Request) {
	nodeIDStr := r.URL.Query().Get("node_id")
	userIDStr := r.URL.Query().Get("user_id")

	if nodeIDStr != "" {
		nodeID, err := uuid.Parse(nodeIDStr)
		if err != nil {
			response.RespondBadRequest(w, r, "Invalid node_id", nil)
			return
		}
		creds, err := h.repo.ListByNode(r.Context(), nodeID)
		if err != nil {
			response.RespondInternalError(w, r, "Failed to retrieve credentials")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(creds)
		return
	}

	if userIDStr != "" {
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			response.RespondBadRequest(w, r, "Invalid user_id", nil)
			return
		}
		creds, err := h.repo.ListByUser(r.Context(), userID)
		if err != nil {
			response.RespondInternalError(w, r, "Failed to retrieve credentials")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(creds)
		return
	}

	response.RespondBadRequest(w, r, "Must specify user_id or node_id query parameter", nil)
}

func (h *CredentialHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	if h.userRepo != nil {
		if _, err := h.userRepo.GetByID(r.Context(), req.UserID); err != nil {
			response.RespondBadRequest(w, r, "User does not exist", map[string]string{"user_id": "not found"})
			return
		}
	}

	if h.nodeRepo != nil {
		if _, err := h.nodeRepo.GetByID(r.Context(), req.NodeID); err != nil {
			response.RespondBadRequest(w, r, "Node does not exist", map[string]string{"node_id": "not found"})
			return
		}
	}

	publicKey := strings.TrimSpace(req.PublicKey)
	var generatedPrivKey string
	if publicKey == "" && (req.Protocol == "wireguard" || req.Protocol == "amneziawg") {
		priv, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			response.RespondInternalError(w, r, "Failed to generate WireGuard keypair")
			return
		}
		publicKey = priv.PublicKey().String()
		generatedPrivKey = priv.String()
	}

	var parsedV4 *netip.Addr
	if req.IPv4 != "" {
		if addr, err := netip.ParseAddr(strings.TrimSpace(req.IPv4)); err == nil {
			parsedV4 = &addr
		}
	}

	var parsedV6 *netip.Addr
	if req.IPv6 != "" {
		if addr, err := netip.ParseAddr(strings.TrimSpace(req.IPv6)); err == nil {
			parsedV6 = &addr
		}
	}

	var awgJc, awgJmin, awgJmax, awgS1, awgS2 pgtype.Int4
	var awgH1, awgH2, awgH3, awgH4 pgtype.Int8

	if req.Protocol == "amneziawg" {
		jc := int32(4)
		if req.AwgJc != nil {
			jc = *req.AwgJc
		}
		awgJc = pgtype.Int4{Int32: jc, Valid: true}

		jmin := int32(40)
		if req.AwgJmin != nil {
			jmin = *req.AwgJmin
		}
		awgJmin = pgtype.Int4{Int32: jmin, Valid: true}

		jmax := int32(70)
		if req.AwgJmax != nil {
			jmax = *req.AwgJmax
		}
		awgJmax = pgtype.Int4{Int32: jmax, Valid: true}

		s1 := int32(64)
		if req.AwgS1 != nil {
			s1 = *req.AwgS1
		}
		awgS1 = pgtype.Int4{Int32: s1, Valid: true}

		s2 := int32(64)
		if req.AwgS2 != nil {
			s2 = *req.AwgS2
		}
		awgS2 = pgtype.Int4{Int32: s2, Valid: true}

		h1 := int64(16843009)
		if req.AwgH1 != nil {
			h1 = *req.AwgH1
		}
		awgH1 = pgtype.Int8{Int64: h1, Valid: true}

		h2 := int64(33686018)
		if req.AwgH2 != nil {
			h2 = *req.AwgH2
		}
		awgH2 = pgtype.Int8{Int64: h2, Valid: true}

		h3 := int64(50529027)
		if req.AwgH3 != nil {
			h3 = *req.AwgH3
		}
		awgH3 = pgtype.Int8{Int64: h3, Valid: true}

		h4 := int64(67372036)
		if req.AwgH4 != nil {
			h4 = *req.AwgH4
		}
		awgH4 = pgtype.Int8{Int64: h4, Valid: true}
	}

	var credUUID pgtype.UUID
	var credFlow pgtype.Text

	if req.Protocol == "vless" {
		u := uuid.New()
		if req.Uuid != nil && strings.TrimSpace(*req.Uuid) != "" {
			if parsed, parseErr := uuid.Parse(strings.TrimSpace(*req.Uuid)); parseErr == nil {
				u = parsed
			}
		}
		credUUID = pgtype.UUID{Bytes: u, Valid: true}
		flow := "xtls-rprx-vision"
		if req.Flow != nil && strings.TrimSpace(*req.Flow) != "" {
			flow = strings.TrimSpace(*req.Flow)
		}
		credFlow = pgtype.Text{String: flow, Valid: true}
	}

	cred, err := h.repo.Create(r.Context(), store.CreateCredentialParams{
		UserID:       req.UserID,
		NodeID:       req.NodeID,
		Protocol:     req.Protocol,
		PrivateKey:   pgtype.Text{String: generatedPrivKey, Valid: generatedPrivKey != ""},
		PublicKey:    pgtype.Text{String: publicKey, Valid: publicKey != ""},
		PresharedKey: pgtype.Text{},
		Uuid:         credUUID,
		Flow:         credFlow,
		Ipv4:         parsedV4,
		Ipv6:         parsedV6,
		Status:       pgtype.Text{String: "active", Valid: true},
		AwgJc:        awgJc,
		AwgJmin:      awgJmin,
		AwgJmax:      awgJmax,
		AwgS1:        awgS1,
		AwgS2:        awgS2,
		AwgH1:        awgH1,
		AwgH2:        awgH2,
		AwgH3:        awgH3,
		AwgH4:        awgH4,
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to create credential")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "create", "credential", &cred.ID, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(ProvisionCredentialResponse{
		Credential: cred,
		PrivateKey: generatedPrivKey,
	})
}

func (h *CredentialHandler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid credential ID", nil)
		return
	}

	cred, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "Credential not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(cred)
}

func (h *CredentialHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid credential ID", nil)
		return
	}

	if _, err := h.repo.GetByID(r.Context(), id); err != nil {
		response.RespondNotFound(w, r, "Credential not found")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		response.RespondInternalError(w, r, "Failed to delete credential")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "delete", "credential", &id, nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *CredentialHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid credential ID", nil)
		return
	}

	existing, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		response.RespondNotFound(w, r, "Credential not found")
		return
	}

	if existing.Protocol != "wireguard" && existing.Protocol != "amneziawg" {
		response.RespondBadRequest(w, r, "Key rotation is only supported for WireGuard/AmneziaWG protocols", nil)
		return
	}

	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate new keypair")
		return
	}
	newPublicKey := priv.PublicKey().String()

	updated, err := h.repo.Update(r.Context(), store.UpdateCredentialParams{
		ID:           id,
		PrivateKey:   pgtype.Text{String: priv.String(), Valid: true},
		PublicKey:    pgtype.Text{String: newPublicKey, Valid: true},
		PresharedKey: existing.PresharedKey,
		Uuid:         existing.Uuid,
		Password:     existing.Password,
		Email:        existing.Email,
		Flow:         existing.Flow,
		Ipv4:         existing.Ipv4,
		Ipv6:         existing.Ipv6,
		Dns:          existing.Dns,
		Mtu:          existing.Mtu,
		Keepalive:    existing.Keepalive,
		AllowedIps:   existing.AllowedIps,
		Status:       existing.Status,
		ExpiresAt:    existing.ExpiresAt,
		AwgJc:        existing.AwgJc,
		AwgJmin:      existing.AwgJmin,
		AwgJmax:      existing.AwgJmax,
		AwgS1:        existing.AwgS1,
		AwgS2:        existing.AwgS2,
		AwgH1:        existing.AwgH1,
		AwgH2:        existing.AwgH2,
		AwgH3:        existing.AwgH3,
		AwgH4:        existing.AwgH4,
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to rotate credential keys")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "rotate_keys", "credential", &id, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ProvisionCredentialResponse{
		Credential: updated,
		PrivateKey: priv.String(),
	})
}

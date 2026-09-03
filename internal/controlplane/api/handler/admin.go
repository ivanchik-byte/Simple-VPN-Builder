package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/response"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

type AdminHandler struct {
	adminRepo     store.AdminRepository
	apiKeyRepo    store.APIKeyRepository
	apiKeyManager *auth.APIKeyManager
	pwdManager    *auth.PasswordManager
	audit         *middleware.AuditService
}

func NewAdminHandler(
	adminRepo store.AdminRepository,
	apiKeyRepo store.APIKeyRepository,
	apiKeyManager *auth.APIKeyManager,
	pwdManager *auth.PasswordManager,
	audit *middleware.AuditService,
) *AdminHandler {
	return &AdminHandler{
		adminRepo:     adminRepo,
		apiKeyRepo:    apiKeyRepo,
		apiKeyManager: apiKeyManager,
		pwdManager:    pwdManager,
		audit:         audit,
	}
}

type CreateAdminRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8,max=128"`
	Role     string `json:"role" validate:"required,oneof=admin superadmin"`
}

type AdminResponse struct {
	ID          uuid.UUID          `json:"id"`
	Email       string             `json:"email"`
	Role        string             `json:"role"`
	TotpEnabled bool               `json:"totp_enabled"`
	LastLogin   pgtype.Timestamptz `json:"last_login"`
	CreatedAt   pgtype.Timestamptz `json:"created_at"`
}

func (h *AdminHandler) ListAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := h.adminRepo.List(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve admins")
		return
	}

	result := make([]AdminResponse, 0, len(admins))
	for _, a := range admins {
		result = append(result, AdminResponse{
			ID:          a.ID,
			Email:       a.Email,
			Role:        a.Role.String,
			TotpEnabled: a.TotpSecret.Valid && a.TotpSecret.String != "",
			LastLogin:   a.LastLogin,
			CreatedAt:   a.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

func (h *AdminHandler) CreateAdmin(w http.ResponseWriter, r *http.Request) {
	var req CreateAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	hash, err := h.pwdManager.Hash(req.Password)
	if err != nil {
		response.RespondInternalError(w, r, "Failed to hash password")
		return
	}

	admin, err := h.adminRepo.Create(r.Context(), store.CreateAdminParams{
		Email:        strings.TrimSpace(strings.ToLower(req.Email)),
		PasswordHash: hash,
		Role:         pgtype.Text{String: req.Role, Valid: true},
	})
	if err != nil {
		response.RespondConflict(w, r, "Admin with this email already exists")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "create", "admin", &admin.ID, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(AdminResponse{
		ID:          admin.ID,
		Email:       admin.Email,
		Role:        admin.Role.String,
		TotpEnabled: admin.TotpSecret.Valid && admin.TotpSecret.String != "",
		LastLogin:   admin.LastLogin,
		CreatedAt:   admin.CreatedAt,
	})
}

type CreateAPIKeyRequest struct {
	Name      string     `json:"name" validate:"required,min=2,max=64"`
	Scopes    []string   `json:"scopes" validate:"required,min=1"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type CreateAPIKeyResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	RawKey    string    `json:"raw_key"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *AdminHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	authCtx := middleware.GetAuth(r.Context())
	if authCtx == nil {
		response.RespondUnauthorized(w, r, "Authentication required")
		return
	}

	keys, err := h.apiKeyRepo.List(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve API keys")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(keys)
}

func (h *AdminHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	authCtx := middleware.GetAuth(r.Context())
	if authCtx == nil {
		response.RespondUnauthorized(w, r, "Authentication required")
		return
	}
	if authCtx.AuthType != "jwt" {
		response.RespondForbidden(w, r, "API keys cannot issue other API keys; admin session required")
		return
	}

	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondBadRequest(w, r, "Invalid JSON body", nil)
		return
	}

	if ok, errMap := request.ValidateStruct(req); !ok {
		response.RespondBadRequest(w, r, "Validation failed", errMap)
		return
	}

	rawKey, keyHash, err := h.apiKeyManager.GenerateKey()
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate API key")
		return
	}

	prefix := rawKey[:8]

	var expTime pgtype.Timestamptz
	if req.ExpiresAt != nil {
		expTime = pgtype.Timestamptz{Time: *req.ExpiresAt, Valid: true}
	}

	apiKey, err := h.apiKeyRepo.Create(r.Context(), store.CreateAPIKeyParams{
		Name:      strings.TrimSpace(req.Name),
		KeyHash:   keyHash,
		Prefix:    prefix,
		Scopes:    req.Scopes,
		ExpiresAt: expTime,
		CreatedBy: pgtype.UUID{Bytes: authCtx.UserID, Valid: true},
	})
	if err != nil {
		response.RespondInternalError(w, r, "Failed to persist API key")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "create", "api_key", &apiKey.ID, nil)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(CreateAPIKeyResponse{
		ID:        apiKey.ID,
		Name:      apiKey.Name,
		Prefix:    prefix,
		RawKey:    rawKey,
		Scopes:    apiKey.Scopes,
		CreatedAt: apiKey.CreatedAt.Time,
	})
}

func (h *AdminHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.RespondBadRequest(w, r, "Invalid API key ID", nil)
		return
	}

	if err := h.apiKeyRepo.Delete(r.Context(), id); err != nil {
		response.RespondNotFound(w, r, "API key not found")
		return
	}

	if h.audit != nil {
		_ = h.audit.Log(r, "delete", "api_key", &id, nil)
	}

	w.WriteHeader(http.StatusNoContent)
}

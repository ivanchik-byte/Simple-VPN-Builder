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
	Role     string `json:"role" validate:"required,oneof=owner superadmin admin"`
}

// callerCanKeys reports whether the caller may view and manage developer API keys.
// Service keys keep their prior access; human admins need owner/superadmin role
// or an explicit grant.
func (h *AdminHandler) callerCanKeys(r *http.Request) bool {
	authCtx := middleware.GetAuth(r.Context())
	if authCtx == nil {
		return false
	}
	if authCtx.Role == "owner" || authCtx.Role == "superadmin" {
		return true
	}
	if authCtx.AuthType != "jwt" {
		return authCtx.HasScope("admin") || authCtx.HasScope("*")
	}
	admin, err := h.adminRepo.GetByID(r.Context(), authCtx.UserID)
	if err != nil {
		return false
	}
	return admin.ParsedPermissions().CanViewAPIKeys
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

	// Explicit role-hierarchy enforcement (defense in depth behind RequireRole):
	// only JWT sessions may create admins; a superadmin cannot create
	// owner/superadmin accounts; a plain admin cannot create anyone.
	authCtx := middleware.GetAuth(r.Context())
	if authCtx == nil || authCtx.AuthType != "jwt" {
		response.RespondForbidden(w, r, "Admin creation requires an admin session")
		return
	}
	callerRole := authCtx.Role
	if callerAdmin, err := h.adminRepo.GetByID(r.Context(), authCtx.UserID); err == nil && callerAdmin.Role.Valid && callerAdmin.Role.String != "" {
		callerRole = callerAdmin.Role.String
	}
	if callerRole != "owner" && callerRole != "superadmin" {
		if h.audit != nil {
			diffBytes, _ := json.Marshal(map[string]string{
				"caller_role": callerRole,
				"target_role": req.Role,
				"target_mail": strings.TrimSpace(strings.ToLower(req.Email)),
			})
			_ = h.audit.Log(r, "create_admin_denied", "admin", nil, diffBytes)
		}
		response.RespondForbidden(w, r, "Admin creation requires owner or superadmin role")
		return
	}
	if callerRole == "superadmin" && (req.Role == "owner" || req.Role == "superadmin") {
		if h.audit != nil {
			diffBytes, _ := json.Marshal(map[string]string{
				"caller_role": callerRole,
				"target_role": req.Role,
				"target_mail": strings.TrimSpace(strings.ToLower(req.Email)),
			})
			_ = h.audit.Log(r, "create_admin_denied", "admin", nil, diffBytes)
		}
		response.RespondForbidden(w, r, "Superadmins can only create regular admin accounts")
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
	if !h.callerCanKeys(r) {
		response.RespondForbidden(w, r, "Viewing API keys requires owner role or key permission")
		return
	}

	keys, err := h.apiKeyRepo.List(r.Context())
	if err != nil {
		response.RespondInternalError(w, r, "Failed to retrieve API keys")
		return
	}

	// Never expose key hashes: they are useless to viewers and expand the
	// attack surface on backup/log leaks.
	type apiKeyView struct {
		ID         uuid.UUID          `json:"id"`
		Name       string             `json:"name"`
		Prefix     string             `json:"prefix"`
		Scopes     []string           `json:"scopes"`
		ExpiresAt  pgtype.Timestamptz `json:"expires_at"`
		LastUsedAt pgtype.Timestamptz `json:"last_used_at"`
		CreatedBy  pgtype.UUID        `json:"created_by"`
		CreatedAt  pgtype.Timestamptz `json:"created_at"`
	}
	result := make([]apiKeyView, 0, len(keys))
	for _, k := range keys {
		result = append(result, apiKeyView{
			ID:         k.ID,
			Name:       k.Name,
			Prefix:     k.Prefix,
			Scopes:     k.Scopes,
			ExpiresAt:  k.ExpiresAt,
			LastUsedAt: k.LastUsedAt,
			CreatedBy:  k.CreatedBy,
			CreatedAt:  k.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}

// allowedKeyScopes is the closed allowlist for API key scopes.
var allowedKeyScopes = map[string]bool{
	"admin": true, "*": true,
	"user:read": true, "user:write": true,
	"node:read": true, "node:write": true,
	"billing:read": true, "billing:write": true,
	"ai": true,
}

// normalizeKeyScopes lowercases/trims requested scopes and rejects unknowns.
func normalizeKeyScopes(in []string) ([]string, bool) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		if !allowedKeyScopes[s] {
			return nil, false
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// issuerCanGrant ensures a key never exceeds the issuer's own rights.
// Returns "" when granting is allowed, otherwise a denial reason.
func (h *AdminHandler) issuerCanGrant(r *http.Request, authCtx *middleware.AuthContext, scopes []string) string {
	if authCtx.Role == "owner" {
		return ""
	}
	issuer, err := h.adminRepo.GetByID(r.Context(), authCtx.UserID)
	if err != nil {
		return "Cannot resolve issuer permissions"
	}
	perms := issuer.ParsedPermissions()
	for _, s := range scopes {
		switch s {
		case "admin", "*":
			return "Only owners may issue admin/* keys"
		case "billing:read", "billing:write":
			if !perms.CanManageBilling {
				return "Issuing billing scopes requires the billing permission"
			}
		case "node:read", "node:write":
			if !perms.CanManageNodes {
				return "Issuing node scopes requires the node permission"
			}
		case "user:read", "user:write":
			if !perms.CanManageUsers {
				return "Issuing user scopes requires the user permission"
			}
		case "ai":
			if !perms.CanAccessAICopilot {
				return "Issuing the ai scope requires AI Copilot access"
			}
		}
	}
	return ""
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
	if !h.callerCanKeys(r) {
		response.RespondForbidden(w, r, "Issuing API keys requires owner role or key permission")
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

	// Scope allowlist + issuer cap: a key cannot exceed the issuer's own
	// rights, so a manager holding only CanViewAPIKeys cannot mint "*".
	scopes, ok := normalizeKeyScopes(req.Scopes)
	if !ok {
		response.RespondBadRequest(w, r, "Unknown scope requested; allowed: admin, *, user:read, user:write, node:read, node:write, billing:read, billing:write, ai", nil)
		return
	}
	if errMsg := h.issuerCanGrant(r, authCtx, scopes); errMsg != "" {
		response.RespondForbidden(w, r, errMsg)
		return
	}

	rawKey, keyHash, err := h.apiKeyManager.GenerateKey()
	if err != nil {
		response.RespondInternalError(w, r, "Failed to generate API key")
		return
	}

	prefix := rawKey[:12]

	var expTime pgtype.Timestamptz
	if req.ExpiresAt != nil {
		expTime = pgtype.Timestamptz{Time: *req.ExpiresAt, Valid: true}
	}

	apiKey, err := h.apiKeyRepo.Create(r.Context(), store.CreateAPIKeyParams{
		Name:      strings.TrimSpace(req.Name),
		KeyHash:   keyHash,
		Prefix:    prefix,
		Scopes:    scopes,
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
	if !h.callerCanKeys(r) {
		response.RespondForbidden(w, r, "Revoking API keys requires owner role or key permission")
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

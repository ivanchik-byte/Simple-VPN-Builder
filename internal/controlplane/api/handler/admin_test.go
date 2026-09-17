package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/middleware"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/auth"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAPIKeyRepo struct {
	keys map[uuid.UUID]store.ApiKey
}

func newMockAPIKeyRepo() *mockAPIKeyRepo {
	return &mockAPIKeyRepo{keys: make(map[uuid.UUID]store.ApiKey)}
}

func (m *mockAPIKeyRepo) Create(_ context.Context, params store.CreateAPIKeyParams) (store.ApiKey, error) {
	id := uuid.New()
	k := store.ApiKey{
		ID:        id,
		Name:      params.Name,
		Prefix:    params.Prefix,
		KeyHash:   params.KeyHash,
		Scopes:    params.Scopes,
		CreatedBy: params.CreatedBy,
	}
	m.keys[id] = k
	return k, nil
}
func (m *mockAPIKeyRepo) GetByPrefix(_ context.Context, prefix string) (store.ApiKey, error) {
	for _, k := range m.keys {
		if k.Prefix == prefix {
			return k, nil
		}
	}
	return store.ApiKey{}, errors.New("api key not found")
}
func (m *mockAPIKeyRepo) List(_ context.Context) ([]store.ApiKey, error) {
	var list []store.ApiKey
	for _, k := range m.keys {
		list = append(list, k)
	}
	return list, nil
}
func (m *mockAPIKeyRepo) Update(_ context.Context, _ store.UpdateAPIKeyParams) (store.ApiKey, error) {
	return store.ApiKey{}, nil
}
func (m *mockAPIKeyRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.keys, id)
	return nil
}

func (m *mockAPIKeyRepo) DeleteByAdminID(_ context.Context, adminID uuid.UUID) error {
	for id, k := range m.keys {
		if k.CreatedBy.Valid && k.CreatedBy.Bytes == adminID {
			delete(m.keys, id)
		}
	}
	return nil
}

func (m *mockAPIKeyRepo) GetAPIKeyByPrefix(ctx context.Context, prefix string) (store.ApiKey, error) {
	return m.GetByPrefix(ctx, prefix)
}

func TestAdminHandler_CRUD(t *testing.T) {
	adminRepo := newMockAdminRepo()
	apiKeyRepo := newMockAPIKeyRepo()
	apiKeyManager := auth.NewAPIKeyManager(apiKeyRepo)
	pwdManager := auth.NewPasswordManager(4)

	handler := NewAdminHandler(adminRepo, apiKeyRepo, apiKeyManager, pwdManager, nil)

	r := chi.NewRouter()
	r.Get("/admins", handler.ListAdmins)
	r.Post("/admins", handler.CreateAdmin)
	r.Get("/api-keys", handler.ListAPIKeys)
	r.Post("/api-keys", handler.CreateAPIKey)
	r.Delete("/api-keys/{id}", handler.DeleteAPIKey)

	// 1. Create Admin (seeded owner creates a plain admin)
	seedOwner, err := adminRepo.Create(context.Background(), store.CreateAdminParams{
		Email:        "root@vpn.test",
		PasswordHash: "seed-hash",
		Role:         pgtype.Text{String: "owner", Valid: true},
	})
	require.NoError(t, err)
	ownerCtx := &middleware.AuthContext{
		UserID:   seedOwner.ID,
		Email:    "root@vpn.test",
		Role:     "owner",
		AuthType: "jwt",
	}
	createAdminBody, _ := json.Marshal(CreateAdminRequest{
		Email:    "ops@vpn.test",
		Password: "strongpassword123",
		Role:     "admin",
	})
	req := httptest.NewRequest(http.MethodPost, "/admins", bytes.NewReader(createAdminBody))
	req = req.WithContext(context.WithValue(req.Context(), middleware.AuthCtxKey, ownerCtx))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var adminResp AdminResponse
	err = json.Unmarshal(rec.Body.Bytes(), &adminResp)
	require.NoError(t, err)
	assert.Equal(t, "ops@vpn.test", adminResp.Email)

	// 2. List Admins
	req = httptest.NewRequest(http.MethodGet, "/admins", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Grant key + node/user permissions to the plain admin (mirrors the Permissions modal).
	// Issuer cap: a key cannot exceed the issuer's own grants.
	grantAdmin := adminRepo.admins[adminResp.ID]
	grantAdmin.Permissions = []byte(`{"can_view_api_keys":true,"can_manage_nodes":true,"can_manage_users":true}`)
	adminRepo.admins[adminResp.ID] = grantAdmin

	// A second plain admin without the grant must be denied.
	plainBody, _ := json.Marshal(CreateAdminRequest{
		Email:    "viewer@vpn.test",
		Password: "strongpassword123",
		Role:     "admin",
	})
	plainReq := httptest.NewRequest(http.MethodPost, "/admins", bytes.NewReader(plainBody))
	plainReq = plainReq.WithContext(context.WithValue(plainReq.Context(), middleware.AuthCtxKey, ownerCtx))
	plainRec := httptest.NewRecorder()
	r.ServeHTTP(plainRec, plainReq)
	require.Equal(t, http.StatusCreated, plainRec.Code)
	var plainResp AdminResponse
	require.NoError(t, json.Unmarshal(plainRec.Body.Bytes(), &plainResp))
	deniedCtx := &middleware.AuthContext{
		UserID:   plainResp.ID,
		Email:    "viewer@vpn.test",
		Role:     "admin",
		AuthType: "jwt",
	}
	deniedReq := httptest.NewRequest(http.MethodGet, "/api-keys", nil)
	deniedReq = deniedReq.WithContext(context.WithValue(deniedReq.Context(), middleware.AuthCtxKey, deniedCtx))
	deniedRec := httptest.NewRecorder()
	r.ServeHTTP(deniedRec, deniedReq)
	assert.Equal(t, http.StatusForbidden, deniedRec.Code)

	// Context for API key operations
	adminID := adminResp.ID
	authCtx := &middleware.AuthContext{
		UserID:   adminID,
		Email:    "ops@vpn.test",
		Role:     "admin",
		AuthType: "jwt",
	}

	// 3. Create API Key
	createKeyBody, _ := json.Marshal(CreateAPIKeyRequest{
		Name:   "ci-deployer",
		Scopes: []string{"node:write", "user:read"},
	})
	req = httptest.NewRequest(http.MethodPost, "/api-keys", bytes.NewReader(createKeyBody))
	req = req.WithContext(context.WithValue(req.Context(), middleware.AuthCtxKey, authCtx))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var keyResp CreateAPIKeyResponse
	err = json.Unmarshal(rec.Body.Bytes(), &keyResp)
	require.NoError(t, err)
	assert.Equal(t, "ci-deployer", keyResp.Name)
	validatedKey, valErr := apiKeyManager.ValidateKey(context.Background(), keyResp.RawKey)
	require.NoError(t, valErr)
	assert.NotNil(t, validatedKey)

	// 4. List API Keys
	req = httptest.NewRequest(http.MethodGet, "/api-keys", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.AuthCtxKey, authCtx))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 5. Delete API Key
	req = httptest.NewRequest(http.MethodDelete, "/api-keys/"+keyResp.ID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.AuthCtxKey, authCtx))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestAdminHandler_CreateRoleHierarchy(t *testing.T) {
	adminRepo := newMockAdminRepo()
	apiKeyRepo := newMockAPIKeyRepo()
	apiKeyManager := auth.NewAPIKeyManager(apiKeyRepo)
	pwdManager := auth.NewPasswordManager(4)

	handler := NewAdminHandler(adminRepo, apiKeyRepo, apiKeyManager, pwdManager, nil)

	r := chi.NewRouter()
	r.Post("/admins", handler.CreateAdmin)

	ctx := context.Background()
	owner, _ := adminRepo.Create(ctx, store.CreateAdminParams{
		Email:        "owner@vpn.test",
		PasswordHash: "h",
		Role:         pgtype.Text{String: "owner", Valid: true},
	})
	super, _ := adminRepo.Create(ctx, store.CreateAdminParams{
		Email:        "super@vpn.test",
		PasswordHash: "h",
		Role:         pgtype.Text{String: "superadmin", Valid: true},
	})
	plain, _ := adminRepo.Create(ctx, store.CreateAdminParams{
		Email:        "plain@vpn.test",
		PasswordHash: "h",
		Role:         pgtype.Text{String: "admin", Valid: true},
	})

	as := func(id uuid.UUID, role, authType string) context.Context {
		return context.WithValue(ctx, middleware.AuthCtxKey, &middleware.AuthContext{
			UserID:   id,
			Email:    role + "@vpn.test",
			Role:     role,
			AuthType: authType,
		})
	}

	attempt := func(c context.Context, targetRole string) int {
		body, _ := json.Marshal(CreateAdminRequest{
			Email:    "new-" + targetRole + "-" + uuid.New().String()[:8] + "@vpn.test",
			Password: "strongpassword123",
			Role:     targetRole,
		})
		req := httptest.NewRequest(http.MethodPost, "/admins", bytes.NewReader(body)).WithContext(c)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Unauthenticated and API-key callers are denied.
	assert.Equal(t, http.StatusForbidden, attempt(ctx, "admin"))
	assert.Equal(t, http.StatusForbidden, attempt(as(owner.ID, "api_client", "apikey"), "admin"))
	// Plain admin has no access at all.
	assert.Equal(t, http.StatusForbidden, attempt(as(plain.ID, "admin", "jwt"), "admin"))
	// Superadmin can create regular admins but nothing at/above its level.
	assert.Equal(t, http.StatusCreated, attempt(as(super.ID, "superadmin", "jwt"), "admin"))
	assert.Equal(t, http.StatusForbidden, attempt(as(super.ID, "superadmin", "jwt"), "superadmin"))
	assert.Equal(t, http.StatusForbidden, attempt(as(super.ID, "superadmin", "jwt"), "owner"))
	// Owner can create any role.
	assert.Equal(t, http.StatusCreated, attempt(as(owner.ID, "owner", "jwt"), "superadmin"))
}

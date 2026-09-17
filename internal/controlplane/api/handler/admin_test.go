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

	// 1. Create Admin
	createAdminBody, _ := json.Marshal(CreateAdminRequest{
		Email:    "ops@vpn.test",
		Password: "strongpassword123",
		Role:     "admin",
	})
	req := httptest.NewRequest(http.MethodPost, "/admins", bytes.NewReader(createAdminBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var adminResp AdminResponse
	err := json.Unmarshal(rec.Body.Bytes(), &adminResp)
	require.NoError(t, err)
	assert.Equal(t, "ops@vpn.test", adminResp.Email)

	// 2. List Admins
	req = httptest.NewRequest(http.MethodGet, "/admins", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Grant key permission to the plain admin (mirrors the Permissions modal).
	grantAdmin := adminRepo.admins[adminResp.ID]
	grantAdmin.Permissions = []byte(`{"can_view_api_keys":true}`)
	adminRepo.admins[adminResp.ID] = grantAdmin

	// A second plain admin without the grant must be denied.
	plainBody, _ := json.Marshal(CreateAdminRequest{
		Email:    "viewer@vpn.test",
		Password: "strongpassword123",
		Role:     "admin",
	})
	plainReq := httptest.NewRequest(http.MethodPost, "/admins", bytes.NewReader(plainBody))
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
		Scopes: []string{"nodes:write", "users:read"},
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

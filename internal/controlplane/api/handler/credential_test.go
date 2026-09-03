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
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockCredRepo struct {
	creds map[uuid.UUID]store.Credential
}

func newMockCredRepo() *mockCredRepo {
	return &mockCredRepo{creds: make(map[uuid.UUID]store.Credential)}
}

func (m *mockCredRepo) Create(_ context.Context, params store.CreateCredentialParams) (store.Credential, error) {
	id := uuid.New()
	c := store.Credential{
		ID:           id,
		UserID:       params.UserID,
		NodeID:       params.NodeID,
		Protocol:     params.Protocol,
		PublicKey:    params.PublicKey,
		PresharedKey: params.PresharedKey,
		Ipv4:         params.Ipv4,
		Ipv6:         params.Ipv6,
	}
	m.creds[id] = c
	return c, nil
}

func (m *mockCredRepo) GetByID(_ context.Context, id uuid.UUID) (store.Credential, error) {
	if c, ok := m.creds[id]; ok {
		return c, nil
	}
	return store.Credential{}, errors.New("credential not found")
}

func (m *mockCredRepo) GetByUserNodeProtocol(_ context.Context, userID, nodeID uuid.UUID, proto string) (store.Credential, error) {
	for _, c := range m.creds {
		if c.UserID == userID && c.NodeID == nodeID && c.Protocol == proto {
			return c, nil
		}
	}
	return store.Credential{}, errors.New("credential not found")
}

func (m *mockCredRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]store.Credential, error) {
	var list []store.Credential
	for _, c := range m.creds {
		if c.UserID == userID {
			list = append(list, c)
		}
	}
	return list, nil
}

func (m *mockCredRepo) ListByNode(_ context.Context, nodeID uuid.UUID) ([]store.Credential, error) {
	var list []store.Credential
	for _, c := range m.creds {
		if c.NodeID == nodeID {
			list = append(list, c)
		}
	}
	return list, nil
}

func (m *mockCredRepo) ListActiveByNode(_ context.Context, _ uuid.UUID) ([]store.Credential, error) {
	return nil, nil
}

func (m *mockCredRepo) Update(_ context.Context, params store.UpdateCredentialParams) (store.Credential, error) {
	c, ok := m.creds[params.ID]
	if !ok {
		return store.Credential{}, errors.New("credential not found")
	}
	c.PublicKey = params.PublicKey
	m.creds[params.ID] = c
	return c, nil
}

func (m *mockCredRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.creds, id)
	return nil
}

func TestCredentialHandler_CRUD(t *testing.T) {
	credRepo := newMockCredRepo()
	handler := NewCredentialHandler(credRepo, nil, nil, nil)

	r := chi.NewRouter()
	r.Get("/credentials", handler.List)
	r.Post("/credentials", handler.Create)
	r.Get("/credentials/{id}", handler.Get)
	r.Delete("/credentials/{id}", handler.Delete)
	r.Post("/credentials/{id}/rotate", handler.Rotate)

	userID := uuid.New()
	nodeID := uuid.New()

	// 1. Create Credential (auto generates WireGuard keypair)
	createBody, _ := json.Marshal(CreateCredentialRequest{
		UserID:   userID,
		NodeID:   nodeID,
		Protocol: "wireguard",
		IPv4:     "10.8.0.2",
	})
	req := httptest.NewRequest(http.MethodPost, "/credentials", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var provResp ProvisionCredentialResponse
	err := json.Unmarshal(rec.Body.Bytes(), &provResp)
	require.NoError(t, err)
	assert.NotEmpty(t, provResp.PrivateKey)
	assert.NotEmpty(t, provResp.Credential.PublicKey.String)

	credID := provResp.Credential.ID

	// 2. List Credentials by user
	req = httptest.NewRequest(http.MethodGet, "/credentials?user_id="+userID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []store.Credential
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	assert.Len(t, list, 1)

	// 3. Get Credential
	req = httptest.NewRequest(http.MethodGet, "/credentials/"+credID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. Rotate Keys
	req = httptest.NewRequest(http.MethodPost, "/credentials/"+credID.String()+"/rotate", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var rotateResp ProvisionCredentialResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &rotateResp)
	assert.NotEqual(t, provResp.PrivateKey, rotateResp.PrivateKey)
	assert.NotEqual(t, provResp.Credential.PublicKey.String, rotateResp.Credential.PublicKey.String)

	// 5. Delete Credential
	req = httptest.NewRequest(http.MethodDelete, "/credentials/"+credID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

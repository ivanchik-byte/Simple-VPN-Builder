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
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/api/request"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockNodeRepo struct {
	nodes map[uuid.UUID]store.Node
}

func newMockNodeRepo() *mockNodeRepo {
	return &mockNodeRepo{nodes: make(map[uuid.UUID]store.Node)}
}

func (m *mockNodeRepo) Create(_ context.Context, params store.CreateNodeParams) (store.Node, error) {
	id := uuid.New()
	node := store.Node{
		ID:           id,
		Name:         params.Name,
		Endpoint:     params.Endpoint,
		GrpcEndpoint: params.GrpcEndpoint,
		Region:       params.Region,
		CapacityGbps: params.CapacityGbps,
		Status:       params.Status,
		Tags:         params.Tags,
	}
	m.nodes[id] = node
	return node, nil
}

func (m *mockNodeRepo) GetByID(_ context.Context, id uuid.UUID) (store.Node, error) {
	if n, ok := m.nodes[id]; ok {
		return n, nil
	}
	return store.Node{}, errors.New("node not found")
}

func (m *mockNodeRepo) GetByName(_ context.Context, name string) (store.Node, error) {
	for _, n := range m.nodes {
		if n.Name == name {
			return n, nil
		}
	}
	return store.Node{}, errors.New("node not found")
}

func (m *mockNodeRepo) List(_ context.Context, filter store.NodeFilter) ([]store.Node, int64, error) {
	var list []store.Node
	for _, n := range m.nodes {
		if filter.Status != "" && n.Status.String != filter.Status {
			continue
		}
		if filter.Region != "" && n.Region.String != filter.Region {
			continue
		}
		list = append(list, n)
	}
	return list, int64(len(list)), nil
}

func (m *mockNodeRepo) ListActive(_ context.Context) ([]store.Node, error) {
	return nil, nil
}

func (m *mockNodeRepo) Update(_ context.Context, params store.UpdateNodeParams) (store.Node, error) {
	n, ok := m.nodes[params.ID]
	if !ok {
		return store.Node{}, errors.New("node not found")
	}
	n.Name = params.Name
	n.Status = params.Status
	n.CapacityGbps = params.CapacityGbps
	n.Tags = params.Tags
	m.nodes[params.ID] = n
	return n, nil
}

func (m *mockNodeRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.nodes, id)
	return nil
}

func (m *mockNodeRepo) UpdateHeartbeat(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func TestNodeHandler_CRUD(t *testing.T) {
	repo := newMockNodeRepo()
	handler := NewNodeHandler(repo, nil)

	r := chi.NewRouter()
	r.Get("/nodes", handler.List)
	r.Post("/nodes", handler.Create)
	r.Get("/nodes/{id}", handler.Get)
	r.Patch("/nodes/{id}", handler.Update)
	r.Delete("/nodes/{id}", handler.Delete)
	r.Get("/nodes/{id}/stats", handler.GetStats)

	// 1. Create Node
	createBody, _ := json.Marshal(CreateNodeRequest{
		Name:      "fra-exit-01",
		Region:    "eu-central",
		Country:   "DE",
		City:      "Frankfurt",
		PublicIP:  "198.51.100.10",
		Capacity:  1000,
		Protocols: []string{"wireguard"},
		Tags:      []string{"premium"},
	})
	req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var createdNode store.Node
	err := json.Unmarshal(rec.Body.Bytes(), &createdNode)
	require.NoError(t, err)
	assert.Equal(t, "fra-exit-01", createdNode.Name)
	assert.Equal(t, "198.51.100.10:51820", createdNode.Endpoint)

	// 2. List Nodes
	req = httptest.NewRequest(http.MethodGet, "/nodes?page=1&per_page=10", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var listResp request.PaginatedResponse[store.Node]
	err = json.Unmarshal(rec.Body.Bytes(), &listResp)
	require.NoError(t, err)
	assert.Len(t, listResp.Items, 1)

	// 3. Get Node by ID
	req = httptest.NewRequest(http.MethodGet, "/nodes/"+createdNode.ID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. Update Node
	updateBody, _ := json.Marshal(UpdateNodeRequest{Status: "maintenance", Capacity: 1500})
	req = httptest.NewRequest(http.MethodPatch, "/nodes/"+createdNode.ID.String(), bytes.NewReader(updateBody))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var updatedNode store.Node
	_ = json.Unmarshal(rec.Body.Bytes(), &updatedNode)
	assert.Equal(t, "maintenance", updatedNode.Status.String)
	assert.Equal(t, int32(1500), updatedNode.CapacityGbps.Int32)

	// 5. Get Stats
	req = httptest.NewRequest(http.MethodGet, "/nodes/"+createdNode.ID.String()+"/stats", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 6. Delete Node
	req = httptest.NewRequest(http.MethodDelete, "/nodes/"+createdNode.ID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// 7. Get Deleted Node -> 404
	req = httptest.NewRequest(http.MethodGet, "/nodes/"+createdNode.ID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

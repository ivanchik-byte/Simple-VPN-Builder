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

type mockPlanRepo struct {
	plans map[uuid.UUID]store.Plan
}

func newMockPlanRepo() *mockPlanRepo {
	return &mockPlanRepo{plans: make(map[uuid.UUID]store.Plan)}
}

func (m *mockPlanRepo) Create(_ context.Context, params store.CreatePlanParams) (store.Plan, error) {
	id := uuid.New()
	p := store.Plan{
		ID:           id,
		Name:         params.Name,
		MonthlyPrice: params.MonthlyPrice,
		TrafficLimit: params.TrafficLimit,
		DeviceLimit:  params.DeviceLimit,
		Protocols:    params.Protocols,
		Features:     params.Features,
		IsActive:     params.IsActive,
	}
	m.plans[id] = p
	return p, nil
}

func (m *mockPlanRepo) GetByID(_ context.Context, id uuid.UUID) (store.Plan, error) {
	if p, ok := m.plans[id]; ok {
		return p, nil
	}
	return store.Plan{}, errors.New("plan not found")
}

func (m *mockPlanRepo) GetByName(_ context.Context, name string) (store.Plan, error) {
	for _, p := range m.plans {
		if p.Name == name {
			return p, nil
		}
	}
	return store.Plan{}, errors.New("plan not found")
}

func (m *mockPlanRepo) List(_ context.Context) ([]store.Plan, error) {
	var list []store.Plan
	for _, p := range m.plans {
		list = append(list, p)
	}
	return list, nil
}

func (m *mockPlanRepo) ListActive(_ context.Context) ([]store.Plan, error) {
	return m.List(context.Background())
}

func (m *mockPlanRepo) Update(_ context.Context, params store.UpdatePlanParams) (store.Plan, error) {
	p, ok := m.plans[params.ID]
	if !ok {
		return store.Plan{}, errors.New("plan not found")
	}
	p.Name = params.Name
	p.MonthlyPrice = params.MonthlyPrice
	p.DeviceLimit = params.DeviceLimit
	p.Protocols = params.Protocols
	m.plans[params.ID] = p
	return p, nil
}

func (m *mockPlanRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.plans, id)
	return nil
}

func TestPlanHandler_CRUD(t *testing.T) {
	repo := newMockPlanRepo()
	handler := NewPlanHandler(repo, nil)

	r := chi.NewRouter()
	r.Get("/plans", handler.List)
	r.Post("/plans", handler.Create)
	r.Get("/plans/{id}", handler.Get)
	r.Patch("/plans/{id}", handler.Update)
	r.Delete("/plans/{id}", handler.Delete)

	// 1. Create Plan
	createBody, _ := json.Marshal(CreatePlanRequest{
		Name:        "Standard Monthly",
		Price:       "9.99",
		DeviceLimit: 5,
		Protocols:   []string{"wireguard"},
	})
	req := httptest.NewRequest(http.MethodPost, "/plans", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var createdPlan store.Plan
	err := json.Unmarshal(rec.Body.Bytes(), &createdPlan)
	require.NoError(t, err)
	assert.Equal(t, "Standard Monthly", createdPlan.Name)

	// 2. List Plans
	req = httptest.NewRequest(http.MethodGet, "/plans", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []store.Plan
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	assert.Len(t, list, 1)

	// 3. Get Plan
	req = httptest.NewRequest(http.MethodGet, "/plans/"+createdPlan.ID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// 4. Update Plan
	updateBody, _ := json.Marshal(UpdatePlanRequest{Price: "11.99"})
	req = httptest.NewRequest(http.MethodPatch, "/plans/"+createdPlan.ID.String(), bytes.NewReader(updateBody))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var updated store.Plan
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)

	// 5. Delete Plan
	req = httptest.NewRequest(http.MethodDelete, "/plans/"+createdPlan.ID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

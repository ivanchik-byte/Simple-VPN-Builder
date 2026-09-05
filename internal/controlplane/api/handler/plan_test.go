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
		ID:             id,
		Name:           params.Name,
		MonthlyPrice:   params.MonthlyPrice,
		TrafficLimit:   params.TrafficLimit,
		DeviceLimit:    params.DeviceLimit,
		MaxDevices:     params.MaxDevices,
		TrafficLimitGb: params.TrafficLimitGb,
		Price1m:        params.Price1m,
		Price3m:        params.Price3m,
		Price6m:        params.Price6m,
		Price12m:       params.Price12m,
		Protocols:      params.Protocols,
		Features:       params.Features,
		IsActive:       params.IsActive,
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
	if params.MaxDevices.Valid {
		p.MaxDevices = params.MaxDevices
	}
	if params.TrafficLimitGb.Valid {
		p.TrafficLimitGb = params.TrafficLimitGb
	}
	if params.Price1m.Valid {
		p.Price1m = params.Price1m
	}
	if params.Price3m.Valid {
		p.Price3m = params.Price3m
	}
	if params.Price6m.Valid {
		p.Price6m = params.Price6m
	}
	if params.Price12m.Valid {
		p.Price12m = params.Price12m
	}
	p.Protocols = params.Protocols
	if params.TrafficLimit.Valid {
		p.TrafficLimit = params.TrafficLimit
	}
	if params.IsActive.Valid {
		p.IsActive = params.IsActive
	}
	m.plans[params.ID] = p
	return p, nil
}

func (m *mockPlanRepo) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.plans, id)
	return nil
}

func (m *mockPlanRepo) GetTrial(_ context.Context) (store.Plan, error) {
	for _, p := range m.plans {
		if p.IsTrial.Bool {
			return p, nil
		}
	}
	return store.Plan{}, errors.New("no trial plan found")
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

func TestPlanHandler_PlanBuilder(t *testing.T) {
	repo := newMockPlanRepo()
	handler := NewPlanHandler(repo, nil)

	r := chi.NewRouter()
	r.Post("/plans", handler.Create)
	r.Patch("/plans/{id}", handler.Update)

	// Create with full builder fields
	createBody, _ := json.Marshal(CreatePlanRequest{
		Name:           "Custom Gamer Plan",
		MaxDevices:     4,
		TrafficLimitGB: 200,
		Price1m:        "7.50",
		Price3m:        "20.00",
		Price6m:        "36.00",
		Price12m:       "60.00",
		Protocols:      []string{"wireguard", "amneziawg", "vless"},
	})
	req := httptest.NewRequest(http.MethodPost, "/plans", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	var created store.Plan
	err := json.Unmarshal(rec.Body.Bytes(), &created)
	require.NoError(t, err)
	assert.Equal(t, "Custom Gamer Plan", created.Name)
	assert.Equal(t, int32(4), created.MaxDevices.Int32)
	assert.Equal(t, int32(200), created.TrafficLimitGb.Int32)
	assert.Equal(t, "7.50", created.Price1mStr())
	assert.Equal(t, "20.00", created.Price3mStr())
	assert.Equal(t, "36.00", created.Price6mStr())
	assert.Equal(t, "60.00", created.Price12mStr())
	assert.True(t, created.HasProtocol("amneziawg"))

	// Update builder fields
	maxDev := int32(8)
	trafGb := int64(500)
	updateBody, _ := json.Marshal(UpdatePlanRequest{
		MaxDevices:     &maxDev,
		TrafficLimitGB: &trafGb,
		Price1m:        "9.00",
		Protocols:      []string{"vless"},
	})
	req = httptest.NewRequest(http.MethodPatch, "/plans/"+created.ID.String(), bytes.NewReader(updateBody))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var updated store.Plan
	err = json.Unmarshal(rec.Body.Bytes(), &updated)
	require.NoError(t, err)
	assert.Equal(t, int32(8), updated.MaxDevices.Int32)
	assert.Equal(t, int32(500), updated.TrafficLimitGb.Int32)
	assert.Equal(t, "9.00", updated.Price1mStr())
	assert.False(t, updated.HasProtocol("wireguard"))
	assert.True(t, updated.HasProtocol("vless"))
}

func TestPlanHandler_ProtocolValidation(t *testing.T) {
	repo := newMockPlanRepo()
	handler := NewPlanHandler(repo, nil)

	r := chi.NewRouter()
	r.Post("/plans", handler.Create)

	// Invalid protocol should be rejected
	createBody, _ := json.Marshal(CreatePlanRequest{
		Name:      "Invalid Proto Plan",
		Price1m:   "5.00",
		Protocols: []string{"wireguard", "openvpn_invalid"},
	})
	req := httptest.NewRequest(http.MethodPost, "/plans", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Invalid protocol")
}


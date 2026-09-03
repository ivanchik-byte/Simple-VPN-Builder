package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTrafficRepo struct {
	nodeStats []store.GetTrafficAggregateByNodeRow
	userStats []store.TrafficStat
}

func (m *mockTrafficRepo) Upsert(_ context.Context, _ store.UpsertTrafficStatsParams) (store.TrafficStat, error) {
	return store.TrafficStat{}, nil
}
func (m *mockTrafficRepo) GetByUserHour(_ context.Context, _, _ uuid.UUID, _ string, _ time.Time) (store.TrafficStat, error) {
	return store.TrafficStat{}, nil
}
func (m *mockTrafficRepo) ListByUser(_ context.Context, _ uuid.UUID, _, _ time.Time) ([]store.TrafficStat, error) {
	return m.userStats, nil
}
func (m *mockTrafficRepo) GetAggregateByUser(_ context.Context, _ uuid.UUID, _, _ time.Time) (store.GetTrafficAggregateByUserRow, error) {
	return store.GetTrafficAggregateByUserRow{}, nil
}
func (m *mockTrafficRepo) GetAggregateByNode(_ context.Context, _, _ time.Time) ([]store.GetTrafficAggregateByNodeRow, error) {
	return m.nodeStats, nil
}

func TestAnalyticsHandler(t *testing.T) {
	nodeID := uuid.New()
	userID := uuid.New()

	trafficRepo := &mockTrafficRepo{
		nodeStats: []store.GetTrafficAggregateByNodeRow{
			{NodeID: nodeID, TotalRx: 1048576, TotalTx: 2097152},
		},
		userStats: []store.TrafficStat{
			{UserID: userID, NodeID: nodeID, RxBytes: pgtype.Int8{Int64: 524288, Valid: true}, TxBytes: pgtype.Int8{Int64: 1048576, Valid: true}},
		},
	}
	handler := NewAnalyticsHandler(trafficRepo)

	r := chi.NewRouter()
	r.Get("/analytics/overview", handler.Overview)
	r.Get("/analytics/nodes", handler.GetByNode)
	r.Get("/analytics/users/{id}", handler.GetByUser)

	// 1. Overview
	req := httptest.NewRequest(http.MethodGet, "/analytics/overview", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var overview TrafficOverviewResponse
	err := json.Unmarshal(rec.Body.Bytes(), &overview)
	require.NoError(t, err)
	assert.Equal(t, int64(1048576), overview.TotalRxBytes)
	assert.Equal(t, int64(2097152), overview.TotalTxBytes)

	// 2. User series
	req = httptest.NewRequest(http.MethodGet, "/analytics/users/"+userID.String(), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var userStats []store.TrafficStat
	_ = json.Unmarshal(rec.Body.Bytes(), &userStats)
	assert.Len(t, userStats, 1)

	// 3. Node series
	req = httptest.NewRequest(http.MethodGet, "/analytics/nodes", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var nodeStats []store.GetTrafficAggregateByNodeRow
	_ = json.Unmarshal(rec.Body.Bytes(), &nodeStats)
	assert.Len(t, nodeStats, 1)
}

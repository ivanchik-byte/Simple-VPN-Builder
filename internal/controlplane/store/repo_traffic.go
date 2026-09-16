package store

import (
	"context"
	"github.com/google/uuid"
	"time"
)

type TrafficRepository interface {
	Upsert(ctx context.Context, params UpsertTrafficStatsParams) (TrafficStat, error)
	GetByUserHour(ctx context.Context, userID, nodeID uuid.UUID, proto string, hour time.Time) (TrafficStat, error)
	ListByUser(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]TrafficStat, error)
	GetAggregateByUser(ctx context.Context, userID uuid.UUID, from, to time.Time) (GetTrafficAggregateByUserRow, error)
	GetAggregateByNode(ctx context.Context, from, to time.Time) ([]GetTrafficAggregateByNodeRow, error)
}

type trafficRepo struct {
	q *Queries
}

func NewTrafficRepository(q *Queries) TrafficRepository {
	return &trafficRepo{q: q}
}

func (r *trafficRepo) Upsert(ctx context.Context, params UpsertTrafficStatsParams) (TrafficStat, error) {
	return r.q.UpsertTrafficStats(ctx, params)
}

func (r *trafficRepo) GetByUserHour(ctx context.Context, userID, nodeID uuid.UUID, proto string, hour time.Time) (TrafficStat, error) {
	return r.q.GetTrafficStatsByUserHour(ctx, GetTrafficStatsByUserHourParams{
		UserID:     userID,
		NodeID:     nodeID,
		Protocol:   proto,
		HourBucket: hour,
	})
}

func (r *trafficRepo) ListByUser(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]TrafficStat, error) {
	return r.q.ListTrafficStatsByUser(ctx, ListTrafficStatsByUserParams{
		UserID:       userID,
		HourBucket:   from,
		HourBucket_2: to,
	})
}

func (r *trafficRepo) GetAggregateByUser(ctx context.Context, userID uuid.UUID, from, to time.Time) (GetTrafficAggregateByUserRow, error) {
	return r.q.GetTrafficAggregateByUser(ctx, GetTrafficAggregateByUserParams{
		UserID:       userID,
		HourBucket:   from,
		HourBucket_2: to,
	})
}

func (r *trafficRepo) GetAggregateByNode(ctx context.Context, from, to time.Time) ([]GetTrafficAggregateByNodeRow, error) {
	return r.q.GetTrafficAggregateByNode(ctx, GetTrafficAggregateByNodeParams{
		HourBucket:   from,
		HourBucket_2: to,
	})
}

// AdminRepository defines administrator account persistence operations.

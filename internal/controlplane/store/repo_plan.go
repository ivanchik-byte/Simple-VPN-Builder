package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type PlanRepository interface {
	Create(ctx context.Context, params CreatePlanParams) (Plan, error)
	GetByID(ctx context.Context, id uuid.UUID) (Plan, error)
	GetByName(ctx context.Context, name string) (Plan, error)
	GetTrial(ctx context.Context) (Plan, error)
	List(ctx context.Context) ([]Plan, error)
	Update(ctx context.Context, params UpdatePlanParams) (Plan, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type planRepo struct {
	q *Queries
}

func NewPlanRepository(q *Queries) PlanRepository {
	return &planRepo{q: q}
}

func (r *planRepo) Create(ctx context.Context, params CreatePlanParams) (Plan, error) {
	if !params.MaxDevices.Valid || params.MaxDevices.Int32 <= 0 {
		if params.DeviceLimit.Valid && params.DeviceLimit.Int32 > 0 {
			params.MaxDevices = params.DeviceLimit
		} else {
			params.MaxDevices = pgtype.Int4{Int32: 3, Valid: true}
		}
	}
	if !params.DeviceLimit.Valid || params.DeviceLimit.Int32 <= 0 {
		params.DeviceLimit = params.MaxDevices
	}

	if !params.Price1m.Valid {
		params.Price1m = params.MonthlyPrice
	}
	if !params.MonthlyPrice.Valid {
		params.MonthlyPrice = params.Price1m
	}

	if params.TrafficLimitGb.Valid && params.TrafficLimitGb.Int32 > 0 && (!params.TrafficLimit.Valid || params.TrafficLimit.Int64 == 0) {
		params.TrafficLimit = pgtype.Int8{Int64: int64(params.TrafficLimitGb.Int32) * 1024 * 1024 * 1024, Valid: true}
	} else if params.TrafficLimit.Valid && params.TrafficLimit.Int64 > 0 && (!params.TrafficLimitGb.Valid || params.TrafficLimitGb.Int32 == 0) {
		params.TrafficLimitGb = pgtype.Int4{Int32: int32(params.TrafficLimit.Int64 / (1024 * 1024 * 1024)), Valid: true}
	}

	return r.q.CreatePlan(ctx, params)
}

func (r *planRepo) GetByID(ctx context.Context, id uuid.UUID) (Plan, error) {
	return r.q.GetPlanByID(ctx, id)
}

func (r *planRepo) GetByName(ctx context.Context, name string) (Plan, error) {
	return r.q.GetPlanByName(ctx, name)
}

func (r *planRepo) GetTrial(ctx context.Context) (Plan, error) {
	return r.q.GetTrialPlan(ctx)
}

func (r *planRepo) List(ctx context.Context) ([]Plan, error) {
	return r.q.ListPlans(ctx)
}

func (r *planRepo) Update(ctx context.Context, params UpdatePlanParams) (Plan, error) {
	if params.MaxDevices.Valid && params.MaxDevices.Int32 > 0 && (!params.DeviceLimit.Valid || params.DeviceLimit.Int32 <= 0) {
		params.DeviceLimit = params.MaxDevices
	} else if params.DeviceLimit.Valid && params.DeviceLimit.Int32 > 0 && (!params.MaxDevices.Valid || params.MaxDevices.Int32 <= 0) {
		params.MaxDevices = params.DeviceLimit
	}

	if params.Price1m.Valid && (!params.MonthlyPrice.Valid) {
		params.MonthlyPrice = params.Price1m
	} else if params.MonthlyPrice.Valid && (!params.Price1m.Valid) {
		params.Price1m = params.MonthlyPrice
	}

	if params.TrafficLimitGb.Valid && params.TrafficLimitGb.Int32 > 0 && (!params.TrafficLimit.Valid || params.TrafficLimit.Int64 == 0) {
		params.TrafficLimit = pgtype.Int8{Int64: int64(params.TrafficLimitGb.Int32) * 1024 * 1024 * 1024, Valid: true}
	} else if params.TrafficLimit.Valid && params.TrafficLimit.Int64 > 0 && (!params.TrafficLimitGb.Valid || params.TrafficLimitGb.Int32 == 0) {
		params.TrafficLimitGb = pgtype.Int4{Int32: int32(params.TrafficLimit.Int64 / (1024 * 1024 * 1024)), Valid: true}
	}

	return r.q.UpdatePlan(ctx, params)
}

func (r *planRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeletePlan(ctx, id)
}

// CredentialRepository defines credential persistence operations.

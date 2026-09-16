package store

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

type AuditLogRepository interface {
	Create(ctx context.Context, params CreateAuditLogParams) (AuditLog, error)
	List(ctx context.Context, params ListAuditLogsParams) ([]AuditLog, error)
	Count(ctx context.Context, params CountAuditLogsParams) (int64, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) error
}

type auditLogRepo struct {
	q *Queries
}

func NewAuditLogRepository(q *Queries) AuditLogRepository {
	return &auditLogRepo{q: q}
}

func (r *auditLogRepo) Create(ctx context.Context, params CreateAuditLogParams) (AuditLog, error) {
	return r.q.CreateAuditLog(ctx, params)
}

func (r *auditLogRepo) List(ctx context.Context, params ListAuditLogsParams) ([]AuditLog, error) {
	return r.q.ListAuditLogs(ctx, params)
}

func (r *auditLogRepo) Count(ctx context.Context, params CountAuditLogsParams) (int64, error) {
	return r.q.CountAuditLogs(ctx, params)
}

func (r *auditLogRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time) error {
	return r.q.DeleteAuditLogsOlderThan(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
}

// BillingRepository defines commercial orders, gateways, promo codes, and broadcast campaigns persistence.

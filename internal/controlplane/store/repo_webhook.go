package store

import (
	"context"
	"github.com/google/uuid"
)

type WebhookRepository interface {
	Create(ctx context.Context, params CreateWebhookParams) (Webhook, error)
	GetByID(ctx context.Context, id uuid.UUID) (Webhook, error)
	List(ctx context.Context) ([]Webhook, error)
	ListActive(ctx context.Context) ([]Webhook, error)
	Update(ctx context.Context, params UpdateWebhookParams) (Webhook, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type webhookRepo struct {
	q *Queries
}

func NewWebhookRepository(q *Queries) WebhookRepository {
	return &webhookRepo{q: q}
}

func (r *webhookRepo) Create(ctx context.Context, params CreateWebhookParams) (Webhook, error) {
	return r.q.CreateWebhook(ctx, params)
}

func (r *webhookRepo) GetByID(ctx context.Context, id uuid.UUID) (Webhook, error) {
	return r.q.GetWebhookByID(ctx, id)
}

func (r *webhookRepo) List(ctx context.Context) ([]Webhook, error) {
	return r.q.ListWebhooks(ctx)
}

func (r *webhookRepo) ListActive(ctx context.Context) ([]Webhook, error) {
	return r.q.ListActiveWebhooks(ctx)
}

func (r *webhookRepo) Update(ctx context.Context, params UpdateWebhookParams) (Webhook, error) {
	return r.q.UpdateWebhook(ctx, params)
}

func (r *webhookRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteWebhook(ctx, id)
}

// AuditLogRepository defines audit log persistence operations.

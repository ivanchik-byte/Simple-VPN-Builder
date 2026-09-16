package store

import (
	"context"
	"github.com/google/uuid"
)

type AdminRepository interface {
	Create(ctx context.Context, params CreateAdminParams) (Admin, error)
	GetByID(ctx context.Context, id uuid.UUID) (Admin, error)
	GetByEmail(ctx context.Context, email string) (Admin, error)
	List(ctx context.Context) ([]Admin, error)
	Update(ctx context.Context, params UpdateAdminParams) (Admin, error)
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
	UpdatePermissions(ctx context.Context, id uuid.UUID, permissions []byte) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type adminRepo struct {
	q *Queries
}

func NewAdminRepository(q *Queries) AdminRepository {
	return &adminRepo{q: q}
}

func (r *adminRepo) Create(ctx context.Context, params CreateAdminParams) (Admin, error) {
	return r.q.CreateAdmin(ctx, params)
}

func (r *adminRepo) GetByID(ctx context.Context, id uuid.UUID) (Admin, error) {
	return r.q.GetAdminByID(ctx, id)
}

func (r *adminRepo) GetByEmail(ctx context.Context, email string) (Admin, error) {
	return r.q.GetAdminByEmail(ctx, email)
}

func (r *adminRepo) List(ctx context.Context) ([]Admin, error) {
	return r.q.ListAdmins(ctx)
}

func (r *adminRepo) Update(ctx context.Context, params UpdateAdminParams) (Admin, error) {
	return r.q.UpdateAdmin(ctx, params)
}

func (r *adminRepo) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	return r.q.UpdateAdminLastLogin(ctx, id)
}

func (r *adminRepo) UpdatePermissions(ctx context.Context, id uuid.UUID, permissions []byte) error {
	return r.q.UpdateAdminPermissions(ctx, id, permissions)
}

func (r *adminRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, _ = r.q.db.Exec(ctx, "UPDATE audit_logs SET admin_id = NULL WHERE admin_id = $1", id)
	_, _ = r.q.db.Exec(ctx, "DELETE FROM api_keys WHERE created_by = $1", id)
	return r.q.DeleteAdmin(ctx, id)
}

// APIKeyRepository defines API key persistence operations.

type APIKeyRepository interface {
	Create(ctx context.Context, params CreateAPIKeyParams) (ApiKey, error)
	GetByPrefix(ctx context.Context, prefix string) (ApiKey, error)
	List(ctx context.Context) ([]ApiKey, error)
	Update(ctx context.Context, params UpdateAPIKeyParams) (ApiKey, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type apiKeyRepo struct {
	q *Queries
}

func NewAPIKeyRepository(q *Queries) APIKeyRepository {
	return &apiKeyRepo{q: q}
}

func (r *apiKeyRepo) Create(ctx context.Context, params CreateAPIKeyParams) (ApiKey, error) {
	return r.q.CreateAPIKey(ctx, params)
}

func (r *apiKeyRepo) GetByPrefix(ctx context.Context, prefix string) (ApiKey, error) {
	return r.q.GetAPIKeyByPrefix(ctx, prefix)
}

func (r *apiKeyRepo) List(ctx context.Context) ([]ApiKey, error) {
	return r.q.ListAPIKeys(ctx)
}

func (r *apiKeyRepo) Update(ctx context.Context, params UpdateAPIKeyParams) (ApiKey, error) {
	return r.q.UpdateAPIKey(ctx, params)
}

func (r *apiKeyRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteAPIKey(ctx, id)
}

// WebhookRepository defines webhook subscription persistence operations.

package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type NodeRepository interface {
	Create(ctx context.Context, params CreateNodeParams) (Node, error)
	GetByID(ctx context.Context, id uuid.UUID) (Node, error)
	GetByName(ctx context.Context, name string) (Node, error)
	List(ctx context.Context, filter NodeFilter) ([]Node, int64, error)
	ListActive(ctx context.Context) ([]Node, error)
	Update(ctx context.Context, params UpdateNodeParams) (Node, error)
	UpdateHeartbeat(ctx context.Context, id uuid.UUID, status string) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type nodeRepo struct {
	q *Queries
}

func NewNodeRepository(q *Queries) NodeRepository {
	return &nodeRepo{q: q}
}

func (r *nodeRepo) Create(ctx context.Context, params CreateNodeParams) (Node, error) {
	return r.q.CreateNode(ctx, params)
}

func (r *nodeRepo) GetByID(ctx context.Context, id uuid.UUID) (Node, error) {
	return r.q.GetNodeByID(ctx, id)
}

func (r *nodeRepo) GetByName(ctx context.Context, name string) (Node, error) {
	return r.q.GetNodeByName(ctx, name)
}

func (r *nodeRepo) List(ctx context.Context, filter NodeFilter) ([]Node, int64, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	nodes, err := r.q.ListNodes(ctx, ListNodesParams{
		Column1: filter.Status,
		Column2: filter.Region,
		Limit:   limit,
		Offset:  filter.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	count, err := r.q.CountNodes(ctx, CountNodesParams{
		Column1: filter.Status,
		Column2: filter.Region,
	})
	if err != nil {
		return nil, 0, err
	}
	return nodes, count, nil
}

func (r *nodeRepo) ListActive(ctx context.Context) ([]Node, error) {
	return r.q.ListActiveNodes(ctx)
}

func (r *nodeRepo) Update(ctx context.Context, params UpdateNodeParams) (Node, error) {
	return r.q.UpdateNode(ctx, params)
}

func (r *nodeRepo) UpdateHeartbeat(ctx context.Context, id uuid.UUID, status string) error {
	return r.q.UpdateNodeHeartbeat(ctx, UpdateNodeHeartbeatParams{
		ID:     id,
		Status: pgtype.Text{String: status, Valid: status != ""},
	})
}

func (r *nodeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteNode(ctx, id)
}

// UserRepository defines user persistence operations.

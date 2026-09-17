package store

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repositories struct {
	Nodes       NodeRepository
	Users       UserRepository
	Plans       PlanRepository
	Credentials CredentialRepository
	Traffic     TrafficRepository
	Admins      AdminRepository
	APIKeys     APIKeyRepository
	Webhooks    WebhookRepository
	AuditLogs   AuditLogRepository
	Billing     BillingRepository
	Tenants     *TenantRepository
	Tx          Transactor
	Queries     *Queries
}

// NewRepositories constructs a new Repositories container with the provided pgx pool.

func NewRepositories(pool *pgxpool.Pool) *Repositories {
	q := New(pool)
	return &Repositories{
		Nodes:       NewNodeRepository(q),
		Users:       NewUserRepository(q),
		Plans:       NewPlanRepository(q),
		Credentials: NewCredentialRepository(q),
		Traffic:     NewTrafficRepository(q),
		Admins:      NewAdminRepository(q),
		APIKeys:     NewAPIKeyRepository(q),
		Webhooks:    NewWebhookRepository(q),
		AuditLogs:   NewAuditLogRepository(q),
		Billing:     NewBillingRepository(q, pool),
		Tenants:     NewTenantRepository(pool),
		Tx:          NewTxManager(pool),
		Queries:     q,
	}
}

// Filter and pagination types

type NodeFilter struct {
	Status string
	Region string
	Limit  int32
	Offset int32
}

type UserFilter struct {
	Status string
	PlanID *uuid.UUID
	Limit  int32
	Offset int32
}

// NodeRepository defines node persistence operations.

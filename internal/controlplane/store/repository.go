package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repositories struct {
	NodeRepo        NodeRepository
	UserRepo        UserRepository
	PlanRepo        PlanRepository
	CredentialRepo  CredentialRepository
	TrafficRepo     TrafficRepository
	AdminRepo       AdminRepository
	APIKeyRepo      APIKeyRepository
	AuditLogRepo    AuditLogRepository
}

func NewRepositories(pool *pgxpool.Pool) *Repositories {
	return &Repositories{
		NodeRepo:       NewNodeRepo(pool),
		UserRepo:       NewUserRepo(pool),
		PlanRepo:       NewPlanRepo(pool),
		CredentialRepo: NewCredentialRepo(pool),
		TrafficRepo:    NewTrafficRepo(pool),
		AdminRepo:      NewAdminRepo(pool),
		APIKeyRepo:     NewAPIKeyRepo(pool),
		AuditLogRepo:   NewAuditLogRepo(pool),
	}
}

type NodeRepository interface {
	Create(ctx context.Context, node *Node) error
	GetByID(ctx context.Context, id uuid.UUID) (*Node, error)
	GetByName(ctx context.Context, name string) (*Node, error)
	List(ctx context.Context, filter NodeFilter) ([]*Node, error)
	Update(ctx context.Context, node *Node) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	List(ctx context.Context, filter UserFilter) ([]*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type PlanRepository interface {
	Create(ctx context.Context, plan *Plan) error
	GetByID(ctx context.Context, id uuid.UUID) (*Plan, error)
	GetByName(ctx context.Context, name string) (*Plan, error)
	List(ctx context.Context) ([]*Plan, error)
	Update(ctx context.Context, plan *Plan) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type CredentialRepository interface {
	Create(ctx context.Context, cred *Credential) error
	GetByID(ctx context.Context, id uuid.UUID) (*Credential, error)
	GetByUserNodeProtocol(ctx context.Context, userID, nodeID uuid.UUID, protocol string) (*Credential, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*Credential, error)
	ListByNode(ctx context.Context, nodeID uuid.UUID) ([]*Credential, error)
	Update(ctx context.Context, cred *Credential) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type TrafficRepository interface {
	UpsertHourly(ctx context.Context, stats *TrafficStats) error
	GetByUserHour(ctx context.Context, userID uuid.UUID, hourBucket time.Time) (*TrafficStats, error)
	ListByUser(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]*TrafficStats, error)
	GetAggregate(ctx context.Context, filter TrafficFilter) (*TrafficAggregate, error)
}

type AdminRepository interface {
	Create(ctx context.Context, admin *Admin) error
	GetByID(ctx context.Context, id uuid.UUID) (*Admin, error)
	GetByEmail(ctx context.Context, email string) (*Admin, error)
	List(ctx context.Context) ([]*Admin, error)
	Update(ctx context.Context, admin *Admin) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type APIKeyRepository interface {
	Create(ctx context.Context, key *APIKey) error
	GetByPrefix(ctx context.Context, prefix string) (*APIKey, error)
	List(ctx context.Context) ([]*APIKey, error)
	Update(ctx context.Context, key *APIKey) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type AuditLogRepository interface {
	Create(ctx context.Context, log *AuditLog) error
	List(ctx context.Context, filter AuditLogFilter) ([]*AuditLog, error)
}

type NodeFilter struct {
	Status string
	Region string
	Limit  int
	Offset int
}

type UserFilter struct {
	Status   string
	PlanID   *uuid.UUID
	Limit    int
	Offset   int
}

type TrafficFilter struct {
	UserID   *uuid.UUID
	NodeID   *uuid.UUID
	Protocol string
	From     time.Time
	To       time.Time
}

type TrafficAggregate struct {
	TotalRX int64
	TotalTX int64
	ByNode  map[uuid.UUID]NodeTraffic
	ByProto map[string]ProtoTraffic
}

type NodeTraffic struct {
	NodeID uuid.UUID
	RX     int64
	TX     int64
}

type ProtoTraffic struct {
	Protocol string
	RX       int64
	TX       int64
}

type AuditLogFilter struct {
	AdminID      *uuid.UUID
	Action       string
	ResourceType string
	From         time.Time
	To           time.Time
	Limit        int
	Offset       int
}
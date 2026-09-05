package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repositories aggregates all domain repositories and transaction manager.
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
		Billing:     NewBillingRepository(q),
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
type UserRepository interface {
	Create(ctx context.Context, params CreateUserParams) (User, error)
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	// GetByIDs fetches multiple users in a single query — use instead of looping GetByID.
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]User, error)
	GetByUsername(ctx context.Context, username string) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	GetBySubscriptionToken(ctx context.Context, token uuid.UUID) (User, error)
	RotateSubscriptionToken(ctx context.Context, id uuid.UUID) (User, error)
	List(ctx context.Context, filter UserFilter) ([]User, int64, error)
	Update(ctx context.Context, params UpdateUserParams) (User, error)
	UpdateTraffic(ctx context.Context, id uuid.UUID, bytes int64) error
	ResetTraffic(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByTelegramID(ctx context.Context, tgID int64) (User, error)
	GetByReferralCode(ctx context.Context, code string) (User, error)
	ExtendSubscription(ctx context.Context, id uuid.UUID, expiresAt time.Time, extraTrafficBytes int64) (User, error)
	SetBanStatus(ctx context.Context, id uuid.UUID, isBanned bool, reason string) error
}

type userRepo struct {
	q *Queries
}

func NewUserRepository(q *Queries) UserRepository {
	return &userRepo{q: q}
}

func (r *userRepo) Create(ctx context.Context, params CreateUserParams) (User, error) {
	return r.q.CreateUser(ctx, params)
}

func (r *userRepo) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	return r.q.GetUserByID(ctx, id)
}

// GetByIDs loads multiple users in a single SQL query using ANY($1::uuid[]).
// Returns only the users that exist; callers must handle missing entries.
func (r *userRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	const q = `SELECT id, email, username, password_hash, status, plan_id,
		traffic_limit, traffic_used, expires_at, subscription_token, note,
		created_at, updated_at, telegram_id, telegram_username, trial_used,
		referrer_id, referral_code, is_banned, ban_reason
		FROM users WHERE id = ANY($1::uuid[])`
	rows, err := r.q.db.Query(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0, len(ids))
	for rows.Next() {
		var u User
		if err := rows.Scan(
			&u.ID, &u.Email, &u.Username, &u.PasswordHash,
			&u.Status, &u.PlanID, &u.TrafficLimit, &u.TrafficUsed,
			&u.ExpiresAt, &u.SubscriptionToken, &u.Note,
			&u.CreatedAt, &u.UpdatedAt,
			&u.TelegramID, &u.TelegramUsername, &u.TrialUsed,
			&u.ReferrerID, &u.ReferralCode, &u.IsBanned, &u.BanReason,
		); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (User, error) {
	return r.q.GetUserByUsername(ctx, username)
}

func (r *userRepo) GetByEmail(ctx context.Context, email string) (User, error) {
	return r.q.GetUserByEmail(ctx, pgtype.Text{String: email, Valid: email != ""})
}

func (r *userRepo) GetBySubscriptionToken(ctx context.Context, token uuid.UUID) (User, error) {
	return r.q.GetUserBySubscriptionToken(ctx, token)
}

func (r *userRepo) RotateSubscriptionToken(ctx context.Context, id uuid.UUID) (User, error) {
	return r.q.RotateUserSubscriptionToken(ctx, id)
}

func (r *userRepo) List(ctx context.Context, filter UserFilter) ([]User, int64, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	var planID uuid.UUID
	if filter.PlanID != nil {
		planID = *filter.PlanID
	}
	users, err := r.q.ListUsers(ctx, ListUsersParams{
		Column1: filter.Status,
		Column2: planID,
		Limit:   limit,
		Offset:  filter.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	count, err := r.q.CountUsers(ctx, CountUsersParams{
		Column1: filter.Status,
		Column2: planID,
	})
	if err != nil {
		return nil, 0, err
	}
	return users, count, nil
}

func (r *userRepo) Update(ctx context.Context, params UpdateUserParams) (User, error) {
	return r.q.UpdateUser(ctx, params)
}

func (r *userRepo) UpdateTraffic(ctx context.Context, id uuid.UUID, bytes int64) error {
	return r.q.UpdateUserTraffic(ctx, UpdateUserTrafficParams{
		ID:          id,
		TrafficUsed: pgtype.Int8{Int64: bytes, Valid: true},
	})
}

func (r *userRepo) ResetTraffic(ctx context.Context, id uuid.UUID) error {
	return r.q.ResetUserTraffic(ctx, id)
}

func (r *userRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteUser(ctx, id)
}

func (r *userRepo) GetByTelegramID(ctx context.Context, tgID int64) (User, error) {
	return r.q.GetUserByTelegramID(ctx, pgtype.Int8{Int64: tgID, Valid: true})
}

func (r *userRepo) GetByReferralCode(ctx context.Context, code string) (User, error) {
	return r.q.GetUserByReferralCode(ctx, pgtype.Text{String: code, Valid: code != ""})
}

func (r *userRepo) ExtendSubscription(ctx context.Context, id uuid.UUID, expiresAt time.Time, extraTrafficBytes int64) (User, error) {
	return r.q.ExtendUserSubscription(ctx, ExtendUserSubscriptionParams{
		ID:           id,
		ExpiresAt:    pgtype.Timestamptz{Time: expiresAt, Valid: !expiresAt.IsZero()},
		TrafficLimit: pgtype.Int8{Int64: extraTrafficBytes, Valid: true},
	})
}

func (r *userRepo) SetBanStatus(ctx context.Context, id uuid.UUID, isBanned bool, reason string) error {
	return r.q.SetUserBanStatus(ctx, SetUserBanStatusParams{
		ID:        id,
		IsBanned:  pgtype.Bool{Bool: isBanned, Valid: true},
		BanReason: pgtype.Text{String: reason, Valid: reason != ""},
	})
}

// PlanRepository defines subscription plan persistence operations.
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
	return r.q.UpdatePlan(ctx, params)
}

func (r *planRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeletePlan(ctx, id)
}

// CredentialRepository defines credential persistence operations.
type CredentialRepository interface {
	Create(ctx context.Context, params CreateCredentialParams) (Credential, error)
	GetByID(ctx context.Context, id uuid.UUID) (Credential, error)
	GetByUserNodeProtocol(ctx context.Context, userID, nodeID uuid.UUID, proto string) (Credential, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]Credential, error)
	ListByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error)
	ListActiveByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error)
	// ListAll returns every credential row, used by the web admin panel.
	ListAll(ctx context.Context) ([]Credential, error)
	Update(ctx context.Context, params UpdateCredentialParams) (Credential, error)
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
}

type credentialRepo struct {
	q *Queries
}

func NewCredentialRepository(q *Queries) CredentialRepository {
	return &credentialRepo{q: q}
}

func (r *credentialRepo) Create(ctx context.Context, params CreateCredentialParams) (Credential, error) {
	return r.q.CreateCredential(ctx, params)
}

func (r *credentialRepo) GetByID(ctx context.Context, id uuid.UUID) (Credential, error) {
	return r.q.GetCredentialByID(ctx, id)
}

func (r *credentialRepo) GetByUserNodeProtocol(ctx context.Context, userID, nodeID uuid.UUID, proto string) (Credential, error) {
	return r.q.GetCredentialByUserNodeProtocol(ctx, GetCredentialByUserNodeProtocolParams{
		UserID:   userID,
		NodeID:   nodeID,
		Protocol: proto,
	})
}

func (r *credentialRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]Credential, error) {
	return r.q.ListCredentialsByUser(ctx, userID)
}

func (r *credentialRepo) ListByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error) {
	return r.q.ListCredentialsByNode(ctx, nodeID)
}

func (r *credentialRepo) ListActiveByNode(ctx context.Context, nodeID uuid.UUID) ([]Credential, error) {
	return r.q.ListActiveCredentialsByNode(ctx, nodeID)
}

func (r *credentialRepo) Update(ctx context.Context, params UpdateCredentialParams) (Credential, error) {
	return r.q.UpdateCredential(ctx, params)
}

func (r *credentialRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeleteCredential(ctx, id)
}

func (r *credentialRepo) ListAll(ctx context.Context) ([]Credential, error) {
	const q = `SELECT id, user_id, node_id, protocol, private_key, public_key, preshared_key,
		uuid, password, email, flow, ipv4, ipv6, dns, mtu, keepalive, allowed_ips,
		status, expires_at, awg_jc, awg_jmin, awg_jmax, awg_s1, awg_s2,
		awg_h1, awg_h2, awg_h3, awg_h4, created_at, updated_at
		FROM credentials ORDER BY created_at DESC LIMIT 500`
	rows, err := r.q.db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Credential{}
	for rows.Next() {
		var i Credential
		if err := rows.Scan(
			&i.ID, &i.UserID, &i.NodeID, &i.Protocol,
			&i.PrivateKey, &i.PublicKey, &i.PresharedKey,
			&i.Uuid, &i.Password, &i.Email, &i.Flow,
			&i.Ipv4, &i.Ipv6, &i.Dns, &i.Mtu, &i.Keepalive, &i.AllowedIps,
			&i.Status, &i.ExpiresAt,
			&i.AwgJc, &i.AwgJmin, &i.AwgJmax, &i.AwgS1, &i.AwgS2,
			&i.AwgH1, &i.AwgH2, &i.AwgH3, &i.AwgH4,
			&i.CreatedAt, &i.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (r *credentialRepo) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	return r.q.DeleteCredentialsByUser(ctx, userID)
}

// TrafficRepository defines traffic metrics persistence operations.
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
type AdminRepository interface {
	Create(ctx context.Context, params CreateAdminParams) (Admin, error)
	GetByID(ctx context.Context, id uuid.UUID) (Admin, error)
	GetByEmail(ctx context.Context, email string) (Admin, error)
	List(ctx context.Context) ([]Admin, error)
	Update(ctx context.Context, params UpdateAdminParams) (Admin, error)
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
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

func (r *adminRepo) Delete(ctx context.Context, id uuid.UUID) error {
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
type AuditLogRepository interface {
	Create(ctx context.Context, params CreateAuditLogParams) (AuditLog, error)
	List(ctx context.Context, params ListAuditLogsParams) ([]AuditLog, error)
	Count(ctx context.Context, params CountAuditLogsParams) (int64, error)
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

// BillingRepository defines commercial orders, gateways, promo codes, and broadcast campaigns persistence.
type BillingRepository interface {
	CreateOrder(ctx context.Context, params CreateOrderParams) (Order, error)
	GetOrderByID(ctx context.Context, id uuid.UUID) (Order, error)
	GetOrderByExternalInvoiceID(ctx context.Context, invoiceID string) (Order, error)
	UpdateOrderStatus(ctx context.Context, id uuid.UUID, status string, paidAt *time.Time) (Order, error)
	ListOrdersByUserID(ctx context.Context, userID uuid.UUID) ([]Order, error)

	GetPaymentGatewayByName(ctx context.Context, name string) (PaymentGateway, error)
	ListPaymentGateways(ctx context.Context) ([]PaymentGateway, error)
	UpsertPaymentGateway(ctx context.Context, params UpsertPaymentGatewayParams) (PaymentGateway, error)

	GetPromoCode(ctx context.Context, code string) (PromoCode, error)
	IncrementPromoCodeUsage(ctx context.Context, id uuid.UUID) error
	CreatePromoCode(ctx context.Context, params CreatePromoCodeParams) (PromoCode, error)
	ListPromoCodes(ctx context.Context) ([]PromoCode, error)

	CreateBroadcastCampaign(ctx context.Context, params CreateBroadcastCampaignParams) (BroadcastCampaign, error)
	GetBroadcastCampaign(ctx context.Context, id uuid.UUID) (BroadcastCampaign, error)
	UpdateBroadcastCampaignStats(ctx context.Context, params UpdateBroadcastCampaignStatsParams) (BroadcastCampaign, error)
	ListBroadcastCampaigns(ctx context.Context) ([]BroadcastCampaign, error)
}

type billingRepo struct {
	q *Queries
}

func NewBillingRepository(q *Queries) BillingRepository {
	return &billingRepo{q: q}
}

func (r *billingRepo) CreateOrder(ctx context.Context, params CreateOrderParams) (Order, error) {
	return r.q.CreateOrder(ctx, params)
}

func (r *billingRepo) GetOrderByID(ctx context.Context, id uuid.UUID) (Order, error) {
	return r.q.GetOrderByID(ctx, id)
}

func (r *billingRepo) GetOrderByExternalInvoiceID(ctx context.Context, invoiceID string) (Order, error) {
	return r.q.GetOrderByExternalInvoiceID(ctx, pgtype.Text{String: invoiceID, Valid: invoiceID != ""})
}

func (r *billingRepo) UpdateOrderStatus(ctx context.Context, id uuid.UUID, status string, paidAt *time.Time) (Order, error) {
	var pt pgtype.Timestamptz
	if paidAt != nil {
		pt = pgtype.Timestamptz{Time: *paidAt, Valid: true}
	}
	return r.q.UpdateOrderStatus(ctx, UpdateOrderStatusParams{
		ID:     id,
		Status: pgtype.Text{String: status, Valid: status != ""},
		PaidAt: pt,
	})
}

func (r *billingRepo) ListOrdersByUserID(ctx context.Context, userID uuid.UUID) ([]Order, error) {
	return r.q.ListOrdersByUserID(ctx, userID)
}

func (r *billingRepo) GetPaymentGatewayByName(ctx context.Context, name string) (PaymentGateway, error) {
	return r.q.GetPaymentGatewayByName(ctx, name)
}

func (r *billingRepo) ListPaymentGateways(ctx context.Context) ([]PaymentGateway, error) {
	return r.q.ListPaymentGateways(ctx)
}

func (r *billingRepo) UpsertPaymentGateway(ctx context.Context, params UpsertPaymentGatewayParams) (PaymentGateway, error) {
	return r.q.UpsertPaymentGateway(ctx, params)
}

func (r *billingRepo) GetPromoCode(ctx context.Context, code string) (PromoCode, error) {
	return r.q.GetPromoCodeByCode(ctx, code)
}

func (r *billingRepo) IncrementPromoCodeUsage(ctx context.Context, id uuid.UUID) error {
	return r.q.IncrementPromoCodeUsage(ctx, id)
}

func (r *billingRepo) CreatePromoCode(ctx context.Context, params CreatePromoCodeParams) (PromoCode, error) {
	return r.q.CreatePromoCode(ctx, params)
}

func (r *billingRepo) ListPromoCodes(ctx context.Context) ([]PromoCode, error) {
	return r.q.ListPromoCodes(ctx)
}

func (r *billingRepo) CreateBroadcastCampaign(ctx context.Context, params CreateBroadcastCampaignParams) (BroadcastCampaign, error) {
	return r.q.CreateBroadcastCampaign(ctx, params)
}

func (r *billingRepo) GetBroadcastCampaign(ctx context.Context, id uuid.UUID) (BroadcastCampaign, error) {
	return r.q.GetBroadcastCampaignByID(ctx, id)
}

func (r *billingRepo) UpdateBroadcastCampaignStats(ctx context.Context, params UpdateBroadcastCampaignStatsParams) (BroadcastCampaign, error) {
	return r.q.UpdateBroadcastCampaignStats(ctx, params)
}

func (r *billingRepo) ListBroadcastCampaigns(ctx context.Context) ([]BroadcastCampaign, error) {
	return r.q.ListBroadcastCampaigns(ctx)
}


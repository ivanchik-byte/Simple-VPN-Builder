package store

import (
	"context"
	"fmt"
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
		Billing:     NewBillingRepository(q),
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
	UpdateEmail(ctx context.Context, id uuid.UUID, email string) (User, error)
	LinkTelegramEmail(ctx context.Context, tgID int64, email string) (User, error)
	RebindTelegramUser(ctx context.Context, email string, tgID int64, tgUsername, firstName, lastName string) (User, error)
	UpdateTraffic(ctx context.Context, id uuid.UUID, bytes int64) error
	ResetTraffic(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetByTelegramID(ctx context.Context, tgID int64) (User, error)
	GetByReferralCode(ctx context.Context, code string) (User, error)
	ExtendSubscription(ctx context.Context, id uuid.UUID, expiresAt time.Time, extraTrafficBytes int64) (User, error)
	SetBanStatus(ctx context.Context, id uuid.UUID, isBanned bool, reason string) error
	UpdateTelegramMetadata(ctx context.Context, id uuid.UUID, tgID int64, tgUsername string, trialUsed bool, referrerID *uuid.UUID, refCode string) (User, error)
	CountReferrals(ctx context.Context, referrerID uuid.UUID) (int64, error)
	ListTelegramIDsForBroadcast(ctx context.Context, segment string) ([]int64, error)
	UpsertTelegramLead(ctx context.Context, params TelegramLeadParams) (User, error)
	CreateEmailVerification(ctx context.Context, tgID int64, email, otpHash, purpose string, ttl time.Duration) (UserEmailVerification, error)
	GetActiveEmailVerification(ctx context.Context, tgID int64, purpose string) (*UserEmailVerification, error)
	RecordVerificationAttempt(ctx context.Context, id uuid.UUID, success bool) error
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

func (r *userRepo) UpdateEmail(ctx context.Context, id uuid.UUID, email string) (User, error) {
	return r.q.UpdateUserEmail(ctx, UpdateUserEmailParams{
		ID:    id,
		Email: pgtype.Text{String: email, Valid: email != ""},
	})
}

func (r *userRepo) LinkTelegramEmail(ctx context.Context, tgID int64, email string) (User, error) {
	return r.q.LinkTelegramEmail(ctx, LinkTelegramEmailParams{
		TelegramID: pgtype.Int8{Int64: tgID, Valid: true},
		Email:      pgtype.Text{String: email, Valid: email != ""},
	})
}

func (r *userRepo) RebindTelegramUser(ctx context.Context, email string, tgID int64, tgUsername, firstName, lastName string) (User, error) {
	return r.q.RebindTelegramUser(ctx, RebindTelegramUserParams{
		Email:             pgtype.Text{String: email, Valid: true},
		TelegramID:        pgtype.Int8{Int64: tgID, Valid: true},
		TelegramUsername:  pgtype.Text{String: tgUsername, Valid: tgUsername != ""},
		TelegramFirstName: pgtype.Text{String: firstName, Valid: firstName != ""},
		TelegramLastName:  pgtype.Text{String: lastName, Valid: lastName != ""},
	})
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

func (r *userRepo) UpdateTelegramMetadata(ctx context.Context, id uuid.UUID, tgID int64, tgUsername string, trialUsed bool, referrerID *uuid.UUID, refCode string) (User, error) {
	var refUUID pgtype.UUID
	if referrerID != nil {
		refUUID = pgtype.UUID{Bytes: *referrerID, Valid: true}
	}
	return r.q.UpdateUserTelegram(ctx, UpdateUserTelegramParams{
		ID:               id,
		TelegramID:       pgtype.Int8{Int64: tgID, Valid: tgID > 0},
		TelegramUsername: pgtype.Text{String: tgUsername, Valid: tgUsername != ""},
		TrialUsed:        pgtype.Bool{Bool: trialUsed, Valid: true},
		ReferrerID:       refUUID,
		ReferralCode:     pgtype.Text{String: refCode, Valid: refCode != ""},
	})
}

func (r *userRepo) CountReferrals(ctx context.Context, referrerID uuid.UUID) (int64, error) {
	return r.q.CountReferralsByUserID(ctx, pgtype.UUID{Bytes: referrerID, Valid: true})
}

func (r *userRepo) ListTelegramIDsForBroadcast(ctx context.Context, segment string) ([]int64, error) {
	switch segment {
	case "leads":
		rows, err := r.q.db.Query(ctx, `
			SELECT telegram_id FROM users 
			WHERE telegram_id IS NOT NULL AND is_banned = false 
			AND (plan_id IS NULL OR plan_id = '00000000-0000-0000-0000-000000000000'::uuid) 
			AND (trial_used = false OR trial_used IS NULL)
		`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var tgID int64
			if err := rows.Scan(&tgID); err == nil {
				ids = append(ids, tgID)
			}
		}
		return ids, nil
	default:
		return r.q.ListUsersForBroadcast(ctx, segment)
	}
}

func (r *userRepo) UpsertTelegramLead(ctx context.Context, p TelegramLeadParams) (User, error) {
	uname := p.TelegramUsername
	if uname == "" {
		uname = fmt.Sprintf("tg_%d", p.TelegramID)
	}

	var referrerUUID pgtype.UUID
	if p.ReferrerCode != "" {
		if refUser, err := r.GetByReferralCode(ctx, p.ReferrerCode); err == nil && refUser.TelegramID.Int64 != p.TelegramID {
			referrerUUID = pgtype.UUID{Bytes: refUser.ID, Valid: true}
		}
	}

	// Upsert query
	query := `
		INSERT INTO users (
			username,
			telegram_id,
			telegram_username,
			telegram_first_name,
			telegram_last_name,
			telegram_language_code,
			status,
			referrer_id,
			last_seen_at,
			is_bot_blocked
		) VALUES (
			$1, $2, $3, $4, $5, $6, 'lead', $7, now(), false
		)
		ON CONFLICT (telegram_id) DO UPDATE SET
			telegram_username      = CASE WHEN EXCLUDED.telegram_username != '' THEN EXCLUDED.telegram_username ELSE users.telegram_username END,
			telegram_first_name    = EXCLUDED.telegram_first_name,
			telegram_last_name     = EXCLUDED.telegram_last_name,
			telegram_language_code = COALESCE(NULLIF(EXCLUDED.telegram_language_code, ''), users.telegram_language_code),
			last_seen_at          = now(),
			is_bot_blocked        = false,
			updated_at            = now()
		RETURNING id, email, username, password_hash, status, plan_id, traffic_limit, traffic_used, expires_at, subscription_token, note, created_at, updated_at, telegram_id, telegram_username, trial_used, referrer_id, referral_code, is_banned, ban_reason
	`
	row := r.q.db.QueryRow(ctx, query,
		uname,
		p.TelegramID,
		p.TelegramUsername,
		p.FirstName,
		p.LastName,
		p.LanguageCode,
		referrerUUID,
	)
	var u User
	err := row.Scan(
		&u.ID,
		&u.Email,
		&u.Username,
		&u.PasswordHash,
		&u.Status,
		&u.PlanID,
		&u.TrafficLimit,
		&u.TrafficUsed,
		&u.ExpiresAt,
		&u.SubscriptionToken,
		&u.Note,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.TelegramID,
		&u.TelegramUsername,
		&u.TrialUsed,
		&u.ReferrerID,
		&u.ReferralCode,
		&u.IsBanned,
		&u.BanReason,
	)
	return u, err
}

func (r *userRepo) CreateEmailVerification(ctx context.Context, tgID int64, email, otpHash, purpose string, ttl time.Duration) (UserEmailVerification, error) {
	_, _ = r.q.db.Exec(ctx, `DELETE FROM user_email_verifications WHERE telegram_id = $1 AND purpose = $2 AND verified_at IS NULL`, tgID, purpose)

	query := `
		INSERT INTO user_email_verifications (telegram_id, email, otp_hash, purpose, attempts_remaining, expires_at, created_at)
		VALUES ($1, $2, $3, $4, 3, now() + $5::interval, now())
		RETURNING id, telegram_id, email, otp_hash, purpose, attempts_remaining, expires_at, verified_at, created_at
	`
	row := r.q.db.QueryRow(ctx, query, tgID, email, otpHash, purpose, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
	var v UserEmailVerification
	var verifiedAt pgtype.Timestamptz
	var expAt pgtype.Timestamptz
	var crAt pgtype.Timestamptz
	err := row.Scan(&v.ID, &v.TelegramID, &v.Email, &v.OTPHash, &v.Purpose, &v.AttemptsRemaining, &expAt, &verifiedAt, &crAt)
	if err != nil {
		return UserEmailVerification{}, err
	}
	v.ExpiresAt = expAt.Time
	v.CreatedAt = crAt.Time
	if verifiedAt.Valid {
		v.VerifiedAt = &verifiedAt.Time
	}
	return v, nil
}

func (r *userRepo) GetActiveEmailVerification(ctx context.Context, tgID int64, purpose string) (*UserEmailVerification, error) {
	query := `
		SELECT id, telegram_id, email, otp_hash, purpose, attempts_remaining, expires_at, verified_at, created_at
		FROM user_email_verifications
		WHERE telegram_id = $1 AND purpose = $2 AND verified_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC
		LIMIT 1
	`
	row := r.q.db.QueryRow(ctx, query, tgID, purpose)
	var v UserEmailVerification
	var verifiedAt pgtype.Timestamptz
	var expAt pgtype.Timestamptz
	var crAt pgtype.Timestamptz
	err := row.Scan(&v.ID, &v.TelegramID, &v.Email, &v.OTPHash, &v.Purpose, &v.AttemptsRemaining, &expAt, &verifiedAt, &crAt)
	if err != nil {
		return nil, err
	}
	v.ExpiresAt = expAt.Time
	v.CreatedAt = crAt.Time
	if verifiedAt.Valid {
		v.VerifiedAt = &verifiedAt.Time
	}
	return &v, nil
}

func (r *userRepo) RecordVerificationAttempt(ctx context.Context, id uuid.UUID, success bool) error {
	if success {
		_, err := r.q.db.Exec(ctx, `UPDATE user_email_verifications SET verified_at = now() WHERE id = $1`, id)
		return err
	}
	_, err := r.q.db.Exec(ctx, `UPDATE user_email_verifications SET attempts_remaining = attempts_remaining - 1 WHERE id = $1`, id)
	return err
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
type BillingRepository interface {
	CreateOrder(ctx context.Context, params CreateOrderParams) (Order, error)
	GetOrderByID(ctx context.Context, id uuid.UUID) (Order, error)
	GetOrderByExternalInvoiceID(ctx context.Context, invoiceID string) (Order, error)
	UpdateOrderStatus(ctx context.Context, id uuid.UUID, status string, paidAt *time.Time) (Order, error)
	ListOrdersByUserID(ctx context.Context, userID uuid.UUID) ([]Order, error)

	GetPaymentGatewayByName(ctx context.Context, name string) (PaymentGateway, error)
	ListPaymentGateways(ctx context.Context) ([]PaymentGateway, error)
	UpsertPaymentGateway(ctx context.Context, params UpsertPaymentGatewayParams) (PaymentGateway, error)
	DeletePaymentGateway(ctx context.Context, name string) error

	GetBillingSettings(ctx context.Context) (BillingSetting, error)
	UpsertBillingSettings(ctx context.Context, params UpsertBillingSettingsParams) (BillingSetting, error)


	GetPromoCode(ctx context.Context, code string) (PromoCode, error)
	IncrementPromoCodeUsage(ctx context.Context, id uuid.UUID) error
	CreatePromoCode(ctx context.Context, params CreatePromoCodeParams) (PromoCode, error)
	ListPromoCodes(ctx context.Context) ([]PromoCode, error)

	CreateBroadcastCampaign(ctx context.Context, params CreateBroadcastCampaignParams) (BroadcastCampaign, error)
	GetBroadcastCampaign(ctx context.Context, id uuid.UUID) (BroadcastCampaign, error)
	UpdateBroadcastCampaignStats(ctx context.Context, params UpdateBroadcastCampaignStatsParams) (BroadcastCampaign, error)
	ListBroadcastCampaigns(ctx context.Context) ([]BroadcastCampaign, error)

	GetBotReplies(ctx context.Context) (map[string]string, error)
	UpsertBotReply(ctx context.Context, key, text string) error
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

func (r *billingRepo) DeletePaymentGateway(ctx context.Context, name string) error {
	_, err := r.q.db.Exec(ctx, "DELETE FROM payment_gateways WHERE name = $1", name)
	return err
}

func (r *billingRepo) GetBillingSettings(ctx context.Context) (BillingSetting, error) {
	row, err := r.q.GetBillingSettings(ctx)
	if err != nil {
		return BillingSetting{
			ID:                   1,
			CryptobotApiToken:    "",
			CryptobotEnabled:     false,
			TelegramStarsEnabled: true,
			StarsPricePerMonth:   250,
			WebhookSecret:        "",
		}, nil
	}
	return BillingSetting{
		ID:                   1,
		CryptobotApiToken:    row.CryptobotApiToken,
		CryptobotEnabled:     row.CryptobotEnabled,
		TelegramStarsEnabled: row.TelegramStarsEnabled,
		StarsPricePerMonth:   row.StarsPricePerMonth,
		WebhookSecret:        row.WebhookSecret,
		UpdatedAt:            row.UpdatedAt,
	}, nil
}

func (r *billingRepo) UpsertBillingSettings(ctx context.Context, params UpsertBillingSettingsParams) (BillingSetting, error) {
	row, err := r.q.UpsertBillingSettings(ctx, params)
	if err != nil {
		return BillingSetting{}, err
	}
	return BillingSetting{
		ID:                   1,
		CryptobotApiToken:    row.CryptobotApiToken,
		CryptobotEnabled:     row.CryptobotEnabled,
		TelegramStarsEnabled: row.TelegramStarsEnabled,
		StarsPricePerMonth:   row.StarsPricePerMonth,
		WebhookSecret:        row.WebhookSecret,
		UpdatedAt:            row.UpdatedAt,
	}, nil
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

func (r *billingRepo) GetBotReplies(ctx context.Context) (map[string]string, error) {
	rows, err := r.q.db.Query(ctx, "SELECT key_name, reply_text FROM bot_replies")
	if err != nil {
		return map[string]string{}, nil
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			result[k] = v
		}
	}
	return result, nil
}

func (r *billingRepo) UpsertBotReply(ctx context.Context, key, text string) error {
	query := `
		INSERT INTO bot_replies (key_name, reply_text, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key_name) DO UPDATE
		SET reply_text = EXCLUDED.reply_text, updated_at = now()
	`
	_, err := r.q.db.Exec(ctx, query, key, text)
	return err
}



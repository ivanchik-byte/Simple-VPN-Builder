package store

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

type UserRepository interface {
	Create(ctx context.Context, params CreateUserParams) (User, error)
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	// GetByIDs fetches multiple users in a single query (use this instead of calling GetByID in a loop).
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

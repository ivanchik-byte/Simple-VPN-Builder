package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAlreadyPaid is returned when a conditional paid transition matches 0 rows
// because the order is already paid (idempotent retry).
var ErrAlreadyPaid = errors.New("order already paid")

type BillingRepository interface {
	CreateOrder(ctx context.Context, params CreateOrderParams) (Order, error)
	GetOrderByID(ctx context.Context, id uuid.UUID) (Order, error)
	GetOrderByExternalInvoiceID(ctx context.Context, invoiceID string) (Order, error)
	UpdateOrderStatus(ctx context.Context, id uuid.UUID, status string, paidAt *time.Time) (Order, error)
	MarkOrderPaidTx(ctx context.Context, orderID uuid.UUID, paidAt time.Time) (Order, error)
	CompletePaidOrderTx(ctx context.Context, orderID uuid.UUID, paidAt time.Time, buyerID uuid.UUID, buyerExpires time.Time, buyerExtraTraffic int64, referrerID *uuid.UUID, referrerExpires *time.Time) (Order, error)
	CountPaidOrdersByUserID(ctx context.Context, userID uuid.UUID) (int64, error)
	ListOrdersByUserID(ctx context.Context, userID uuid.UUID) ([]Order, error)

	GetPaymentGatewayByName(ctx context.Context, name string) (PaymentGateway, error)
	ListPaymentGateways(ctx context.Context) ([]PaymentGateway, error)
	UpsertPaymentGateway(ctx context.Context, params UpsertPaymentGatewayParams) (PaymentGateway, error)
	DeletePaymentGateway(ctx context.Context, name string) error

	GetBillingSettings(ctx context.Context) (BillingSetting, error)
	UpsertBillingSettings(ctx context.Context, params UpsertBillingSettingsParams) (BillingSetting, error)

	GetPromoCode(ctx context.Context, code string) (PromoCode, error)
	ConsumePromoCode(ctx context.Context, id uuid.UUID) (PromoCode, error)
	CreatePromoCode(ctx context.Context, params CreatePromoCodeParams) (PromoCode, error)
	ListPromoCodes(ctx context.Context) ([]PromoCode, error)
	CreateOrderWithPromo(ctx context.Context, params CreateOrderParams, promoID *uuid.UUID) (Order, error)

	CreateBroadcastCampaign(ctx context.Context, params CreateBroadcastCampaignParams) (BroadcastCampaign, error)
	GetBroadcastCampaign(ctx context.Context, id uuid.UUID) (BroadcastCampaign, error)
	UpdateBroadcastCampaignStats(ctx context.Context, params UpdateBroadcastCampaignStatsParams) (BroadcastCampaign, error)
	ListBroadcastCampaigns(ctx context.Context) ([]BroadcastCampaign, error)

	GetBotReplies(ctx context.Context) (map[string]string, error)
	UpsertBotReply(ctx context.Context, key, text string) error
	DeleteBotReply(ctx context.Context, key string) error
	GetBotRepliesRevision(ctx context.Context) (string, error)
}

type billingRepo struct {
	q    *Queries
	pool *pgxpool.Pool
}

func NewBillingRepository(q *Queries, pool *pgxpool.Pool) BillingRepository {
	return &billingRepo{q: q, pool: pool}
}

func (r *billingRepo) CreateOrder(ctx context.Context, params CreateOrderParams) (Order, error) {
	return r.q.CreateOrder(ctx, params)
}

// CreateOrderWithPromo atomically consumes one promo use and creates the order.
// If the promo is exhausted/expired/inactive the whole transaction rolls back
// (pgx.ErrNoRows) so a use is never burned without an order.
func (r *billingRepo) CreateOrderWithPromo(ctx context.Context, params CreateOrderParams, promoID *uuid.UUID) (Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.q.WithTx(tx)
	if promoID != nil {
		if _, err := qtx.ConsumePromoCode(ctx, *promoID); err != nil {
			return Order{}, err
		}
	}
	order, err := qtx.CreateOrder(ctx, params)
	if err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
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

// CountPaidOrdersByUserID counts orders with status='paid' for a user.
// Raw SQL is used directly so no sqlc regeneration is required.
func (r *billingRepo) CountPaidOrdersByUserID(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	if err := r.q.db.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE user_id = $1 AND status = 'paid'`, userID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

const orderColumns = `id, user_id, plan_id, gateway, external_invoice_id, amount, currency, status, duration_months, metadata, paid_at, created_at, updated_at`

func scanOrder(row interface {
	Scan(dest ...any) error
}) (Order, error) {
	var o Order
	err := row.Scan(
		&o.ID, &o.UserID, &o.PlanID, &o.Gateway, &o.ExternalInvoiceID,
		&o.Amount, &o.Currency, &o.Status, &o.DurationMonths, &o.Metadata,
		&o.PaidAt, &o.CreatedAt, &o.UpdatedAt,
	)
	return o, err
}

// MarkOrderPaidTx conditionally flips a single order to paid in one TX.
// 0 affected rows means the order is already paid → ErrAlreadyPaid.
func (r *billingRepo) MarkOrderPaidTx(ctx context.Context, orderID uuid.UUID, paidAt time.Time) (Order, error) {
	if r.pool == nil {
		return Order{}, errors.New("billing repo pool is nil")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	order, err := scanOrder(tx.QueryRow(ctx,
		`UPDATE orders SET status='paid', paid_at=$2, updated_at=now() WHERE id=$1 AND status!='paid' RETURNING `+orderColumns,
		orderID, pgtype.Timestamptz{Time: paidAt, Valid: true}))
	if err != nil {
		var status string
		if qerr := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&status); qerr == nil && status == "paid" {
			return Order{}, ErrAlreadyPaid
		}
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
}

// CompletePaidOrderTx atomically marks the order paid AND extends the buyer
// (plus a 7-day referrer bonus only for the referral's first paid order).
// The referral eligibility (no prior paid orders) is checked inside the same TX,
// so a retry after commit sees ErrAlreadyPaid while a failure leaves the order
// pending and safe to retry.
func (r *billingRepo) CompletePaidOrderTx(ctx context.Context, orderID uuid.UUID, paidAt time.Time, buyerID uuid.UUID, buyerExpires time.Time, buyerExtraTraffic int64, referrerID *uuid.UUID, referrerExpires *time.Time) (Order, error) {
	if r.pool == nil {
		return Order{}, errors.New("billing repo pool is nil")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	order, err := scanOrder(tx.QueryRow(ctx,
		`UPDATE orders SET status='paid', paid_at=$2, updated_at=now() WHERE id=$1 AND status!='paid' RETURNING `+orderColumns,
		orderID, pgtype.Timestamptz{Time: paidAt, Valid: true}))
	if err != nil {
		var status string
		if qerr := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&status); qerr == nil && status == "paid" {
			return Order{}, ErrAlreadyPaid
		}
		return Order{}, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET expires_at=$2, traffic_limit=traffic_limit+$3, updated_at=now() WHERE id=$1`,
		buyerID, pgtype.Timestamptz{Time: buyerExpires, Valid: !buyerExpires.IsZero()}, buyerExtraTraffic); err != nil {
		return Order{}, err
	}
	if referrerID != nil && referrerExpires != nil && !referrerExpires.IsZero() {
		var prior int64
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE user_id=$1 AND status='paid' AND id!=$2`, buyerID, orderID).Scan(&prior); err != nil {
			return Order{}, err
		}
		if prior == 0 {
			if _, err := tx.Exec(ctx,
				`UPDATE users SET expires_at=$2, updated_at=now() WHERE id=$1`,
				*referrerID, pgtype.Timestamptz{Time: *referrerExpires, Valid: true}); err != nil {
				return Order{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
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

// ConsumePromoCode atomically validates and redeems one promo use.
func (r *billingRepo) ConsumePromoCode(ctx context.Context, id uuid.UUID) (PromoCode, error) {
	return r.q.ConsumePromoCode(ctx, id)
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

func (r *billingRepo) DeleteBotReply(ctx context.Context, key string) error {
	_, err := r.q.db.Exec(ctx, "DELETE FROM bot_replies WHERE key_name = $1", key)
	return err
}

// GetBotRepliesRevision returns max(updated_at) as an optimistic-concurrency token.
// Empty string means no customized replies stored yet.
func (r *billingRepo) GetBotRepliesRevision(ctx context.Context) (string, error) {
	var rev pgtype.Timestamptz
	if err := r.q.db.QueryRow(ctx, "SELECT MAX(updated_at) FROM bot_replies").Scan(&rev); err != nil {
		return "", err
	}
	if !rev.Valid {
		return "", nil
	}
	return rev.Time.UTC().Format(time.RFC3339Nano), nil
}

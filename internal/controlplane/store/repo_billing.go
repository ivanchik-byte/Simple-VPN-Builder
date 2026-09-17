package store

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"time"
)

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
	ConsumePromoCode(ctx context.Context, id uuid.UUID) (PromoCode, error)
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

package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tenant represents a white-label partner brand, with dedicated bot, channel, and custom mini-app/domain.
type Tenant struct {
	ID                uuid.UUID `json:"id"`
	AdminID           *uuid.UUID `json:"admin_id,omitempty"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	BotToken          string    `json:"bot_token"`
	BotUsername       string    `json:"bot_username"`
	BotWebhookSecret  string    `json:"bot_webhook_secret"`
	ChannelLink       string    `json:"channel_link"`
	ChannelID         int64     `json:"channel_id"`
	RequireChannelSub bool      `json:"require_channel_sub"`
	SupportLink       string    `json:"support_link"`
	CustomDomain      string    `json:"custom_domain"`
	MiniappURL        string    `json:"miniapp_url"`
	BannerURL         string    `json:"banner_url"`
	WelcomeText       string    `json:"welcome_text"`
	IsActive          bool      `json:"is_active"`
	UserCount         int64     `json:"user_count,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type TenantRepository struct {
	pool *pgxpool.Pool
}

func NewTenantRepository(pool *pgxpool.Pool) *TenantRepository {
	return &TenantRepository{pool: pool}
}

func (r *TenantRepository) List(ctx context.Context) ([]Tenant, error) {
	query := `
		SELECT t.id, t.admin_id, t.name, t.slug, t.bot_token, t.bot_username, 
		       t.bot_webhook_secret, t.channel_link, COALESCE(t.channel_id, 0), 
		       t.require_channel_sub, t.support_link, t.custom_domain, t.miniapp_url, 
		       t.banner_url, t.welcome_text, t.is_active, t.created_at, t.updated_at,
		       COUNT(u.id) as user_count
		FROM tenants t
		LEFT JOIN users u ON u.tenant_id = t.id
		GROUP BY t.id
		ORDER BY t.created_at DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Tenant
	for rows.Next() {
		var t Tenant
		var adminID *uuid.UUID
		var chID int64
		err := rows.Scan(
			&t.ID, &adminID, &t.Name, &t.Slug, &t.BotToken, &t.BotUsername,
			&t.BotWebhookSecret, &t.ChannelLink, &chID,
			&t.RequireChannelSub, &t.SupportLink, &t.CustomDomain, &t.MiniappURL,
			&t.BannerURL, &t.WelcomeText, &t.IsActive, &t.CreatedAt, &t.UpdatedAt,
			&t.UserCount,
		)
		if err != nil {
			return nil, err
		}
		t.AdminID = adminID
		t.ChannelID = chID
		list = append(list, t)
	}
	return list, rows.Err()
}

func (r *TenantRepository) GetByID(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	query := `
		SELECT id, admin_id, name, slug, bot_token, bot_username, 
		       bot_webhook_secret, channel_link, COALESCE(channel_id, 0), 
		       require_channel_sub, support_link, custom_domain, miniapp_url, 
		       banner_url, welcome_text, is_active, created_at, updated_at
		FROM tenants
		WHERE id = $1
	`
	var t Tenant
	var adminID *uuid.UUID
	var chID int64
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&t.ID, &adminID, &t.Name, &t.Slug, &t.BotToken, &t.BotUsername,
		&t.BotWebhookSecret, &t.ChannelLink, &chID,
		&t.RequireChannelSub, &t.SupportLink, &t.CustomDomain, &t.MiniappURL,
		&t.BannerURL, &t.WelcomeText, &t.IsActive, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.AdminID = adminID
	t.ChannelID = chID
	return &t, nil
}

func (r *TenantRepository) Create(ctx context.Context, t *Tenant) error {
	query := `
		INSERT INTO tenants (
			admin_id, name, slug, bot_token, bot_username, channel_link, 
			channel_id, require_channel_sub, support_link, custom_domain, 
			miniapp_url, banner_url, welcome_text, is_active
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
		RETURNING id, bot_webhook_secret, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		t.AdminID, t.Name, t.Slug, t.BotToken, t.BotUsername, t.ChannelLink,
		t.ChannelID, t.RequireChannelSub, t.SupportLink, t.CustomDomain,
		t.MiniappURL, t.BannerURL, t.WelcomeText, t.IsActive,
	).Scan(&t.ID, &t.BotWebhookSecret, &t.CreatedAt, &t.UpdatedAt)
}

func (r *TenantRepository) Update(ctx context.Context, t *Tenant) error {
	query := `
		UPDATE tenants SET
			name = $2,
			bot_token = $3,
			bot_username = $4,
			channel_link = $5,
			channel_id = $6,
			require_channel_sub = $7,
			support_link = $8,
			custom_domain = $9,
			miniapp_url = $10,
			banner_url = $11,
			welcome_text = $12,
			is_active = $13,
			updated_at = now()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query,
		t.ID, t.Name, t.BotToken, t.BotUsername, t.ChannelLink,
		t.ChannelID, t.RequireChannelSub, t.SupportLink, t.CustomDomain,
		t.MiniappURL, t.BannerURL, t.WelcomeText, t.IsActive,
	)
	return err
}

func (r *TenantRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM tenants WHERE id = $1", id)
	return err
}

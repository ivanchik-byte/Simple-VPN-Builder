-- name: CreateOrder :one
INSERT INTO orders (user_id, plan_id, gateway, external_invoice_id, amount, currency, status, duration_months, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetOrderByID :one
SELECT * FROM orders WHERE id = $1;

-- name: GetOrderByExternalInvoiceID :one
SELECT * FROM orders WHERE external_invoice_id = $1;

-- name: UpdateOrderStatus :one
UPDATE orders
SET status = $2, paid_at = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListOrdersByUserID :many
SELECT * FROM orders WHERE user_id = $1 ORDER BY created_at DESC;

-- name: GetPaymentGatewayByName :one
SELECT * FROM payment_gateways WHERE name = $1;

-- name: ListPaymentGateways :many
SELECT * FROM payment_gateways ORDER BY name;

-- name: UpsertPaymentGateway :one
INSERT INTO payment_gateways (name, is_enabled, config_encrypted)
VALUES ($1, $2, $3)
ON CONFLICT (name) DO UPDATE
SET is_enabled = EXCLUDED.is_enabled, config_encrypted = EXCLUDED.config_encrypted, updated_at = now()
RETURNING *;

-- name: GetPromoCodeByCode :one
SELECT * FROM promo_codes WHERE code = $1 AND is_active = true;

-- name: IncrementPromoCodeUsage :exec
UPDATE promo_codes SET used_count = used_count + 1 WHERE id = $1;

-- name: CreatePromoCode :one
INSERT INTO promo_codes (code, discount_percent, discount_amount, bonus_days, bonus_bytes, max_uses, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListPromoCodes :many
SELECT * FROM promo_codes ORDER BY created_at DESC;

-- name: CreateBroadcastCampaign :one
INSERT INTO broadcast_campaigns (title, target_segment, message_text, inline_buttons, total_recipients, status)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBroadcastCampaignByID :one
SELECT * FROM broadcast_campaigns WHERE id = $1;

-- name: UpdateBroadcastCampaignStats :one
UPDATE broadcast_campaigns
SET sent_count = $2, failed_count = $3, status = $4, completed_at = $5
WHERE id = $1
RETURNING *;

-- name: ListBroadcastCampaigns :many
SELECT * FROM broadcast_campaigns ORDER BY created_at DESC;

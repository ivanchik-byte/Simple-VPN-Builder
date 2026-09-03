-- name: CreateWebhook :one
INSERT INTO webhooks (url, secret, events, is_active)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWebhookByID :one
SELECT * FROM webhooks WHERE id = $1;

-- name: ListWebhooks :many
SELECT * FROM webhooks ORDER BY created_at DESC;

-- name: ListActiveWebhooks :many
SELECT * FROM webhooks WHERE is_active = true ORDER BY created_at DESC;

-- name: UpdateWebhook :one
UPDATE webhooks
SET url = $2, secret = $3, events = $4, is_active = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWebhook :exec
DELETE FROM webhooks WHERE id = $1;

-- name: CreateAPIKey :one
INSERT INTO api_keys (name, key_hash, prefix, scopes, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAPIKeyByPrefix :one
SELECT * FROM api_keys WHERE prefix = $1;

-- name: ListAPIKeys :many
SELECT * FROM api_keys ORDER BY created_at DESC;

-- name: UpdateAPIKey :one
UPDATE api_keys
SET name = $2, scopes = $3, expires_at = $4, last_used_at = $5
WHERE id = $1
RETURNING *;

-- name: DeleteAPIKey :exec
DELETE FROM api_keys WHERE id = $1;
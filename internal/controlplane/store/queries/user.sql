-- name: CreateUser :one
INSERT INTO users (email, username, password_hash, status, plan_id, traffic_limit, traffic_used, expires_at, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserBySubscriptionToken :one
SELECT * FROM users WHERE subscription_token = $1;

-- name: RotateUserSubscriptionToken :one
UPDATE users SET subscription_token = gen_random_uuid(), updated_at = now() WHERE id = $1 RETURNING *;

-- name: ListUsers :many
SELECT * FROM users
WHERE ($1 = '' OR status = $1)
AND ($2::uuid IS NULL OR $2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR plan_id = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountUsers :one
SELECT COUNT(*) FROM users
WHERE ($1 = '' OR status = $1)
AND ($2::uuid IS NULL OR $2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR plan_id = $2);

-- name: UpdateUser :one
UPDATE users
SET email = $2, username = $3, password_hash = $4, status = $5, plan_id = $6, traffic_limit = $7, expires_at = $8, note = $9, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateUserTraffic :exec
UPDATE users SET traffic_used = traffic_used + $2, updated_at = now() WHERE id = $1;

-- name: ResetUserTraffic :exec
UPDATE users SET traffic_used = 0, updated_at = now() WHERE id = $1;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
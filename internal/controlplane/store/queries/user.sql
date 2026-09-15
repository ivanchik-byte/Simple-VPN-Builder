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

-- name: GetUserByTelegramID :one
SELECT * FROM users WHERE telegram_id = $1;

-- name: GetUserByReferralCode :one
SELECT * FROM users WHERE referral_code = $1;

-- name: ExtendUserSubscription :one
UPDATE users
SET expires_at = $2, traffic_limit = traffic_limit + $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetUserBanStatus :exec
UPDATE users SET is_banned = $2, ban_reason = $3, status = CASE WHEN $2 = true THEN 'banned' ELSE 'active' END, updated_at = now() WHERE id = $1;

-- name: UpdateUserTelegram :one
UPDATE users
SET telegram_id = $2, telegram_username = $3, trial_used = $4, referrer_id = $5, referral_code = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountReferralsByUserID :one
SELECT COUNT(*) FROM users WHERE referrer_id = $1;


-- name: UpdateUserEmail :one
UPDATE users SET email = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: LinkTelegramEmail :one
UPDATE users SET email = $2, updated_at = now() WHERE telegram_id = $1 RETURNING *;

-- name: RebindTelegramUser :one
UPDATE users
SET telegram_id = $2, telegram_username = $3, telegram_first_name = $4, telegram_last_name = $5, updated_at = now()
WHERE email = $1
RETURNING *;

-- name: ListUsersForBroadcast :many
SELECT telegram_id FROM users
WHERE telegram_id IS NOT NULL AND is_banned = false
AND (
    $1 = 'all'
    OR ($1 = 'active' AND status = 'active' AND expires_at > now())
    OR ($1 = 'expired' AND (status != 'active' OR expires_at <= now()))
);
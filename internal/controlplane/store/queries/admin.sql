-- name: CreateAdmin :one
INSERT INTO admins (email, password_hash, role, totp_secret)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAdminByID :one
SELECT * FROM admins WHERE id = $1;

-- name: GetAdminByEmail :one
SELECT * FROM admins WHERE email = $1;

-- name: ListAdmins :many
SELECT * FROM admins ORDER BY created_at DESC;

-- name: UpdateAdmin :one
UPDATE admins
SET email = $2, password_hash = $3, role = $4, totp_secret = $5, last_login = $6
WHERE id = $1
RETURNING *;

-- name: DeleteAdmin :exec
DELETE FROM admins WHERE id = $1;

-- name: UpdateAdminLastLogin :exec
UPDATE admins SET last_login = now() WHERE id = $1;
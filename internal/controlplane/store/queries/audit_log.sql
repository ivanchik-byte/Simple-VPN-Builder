-- name: CreateAuditLog :one
INSERT INTO audit_logs (admin_id, api_key_id, action, resource_type, resource_id, diff, ip_address, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE ($1::uuid IS NULL OR $1::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR admin_id = $1)
AND ($2 = '' OR action = $2)
AND ($3 = '' OR resource_type = $3)
AND created_at >= $4 AND created_at <= $5
ORDER BY created_at DESC
LIMIT $6 OFFSET $7;

-- name: CountAuditLogs :one
SELECT COUNT(*) FROM audit_logs
WHERE ($1::uuid IS NULL OR $1::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR admin_id = $1)
AND ($2 = '' OR action = $2)
AND ($3 = '' OR resource_type = $3)
AND created_at >= $4 AND created_at <= $5;
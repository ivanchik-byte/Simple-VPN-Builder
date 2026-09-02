-- name: CreateCredential :one
INSERT INTO credentials (user_id, node_id, protocol, private_key, public_key, preshared_key, uuid, password, email, flow, ipv4, ipv6, dns, mtu, keepalive, allowed_ips, status, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
RETURNING *;

-- name: GetCredentialByID :one
SELECT * FROM credentials WHERE id = $1;

-- name: GetCredentialByUserNodeProtocol :one
SELECT * FROM credentials WHERE user_id = $1 AND node_id = $2 AND protocol = $3;

-- name: ListCredentialsByUser :many
SELECT * FROM credentials WHERE user_id = $1 ORDER BY created_at DESC;

-- name: ListCredentialsByNode :many
SELECT * FROM credentials WHERE node_id = $1 ORDER BY created_at DESC;

-- name: UpdateCredential :one
UPDATE credentials
SET private_key = $2, public_key = $3, preshared_key = $4, uuid = $5, password = $6, email = $7, flow = $8, ipv4 = $9, ipv6 = $10, dns = $11, mtu = $12, keepalive = $13, allowed_ips = $14, status = $15, expires_at = $16, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteCredential :exec
DELETE FROM credentials WHERE id = $1;
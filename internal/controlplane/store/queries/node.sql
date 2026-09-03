-- name: CreateNode :one
INSERT INTO nodes (name, endpoint, grpc_endpoint, region, capacity_gbps, status, tags, public_key, cert_fingerprint)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetNodeByID :one
SELECT * FROM nodes WHERE id = $1;

-- name: GetNodeByName :one
SELECT * FROM nodes WHERE name = $1;

-- name: ListNodes :many
SELECT * FROM nodes
WHERE ($1 = '' OR status = $1)
AND ($2 = '' OR region = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountNodes :one
SELECT COUNT(*) FROM nodes
WHERE ($1 = '' OR status = $1)
AND ($2 = '' OR region = $2);

-- name: ListActiveNodes :many
SELECT * FROM nodes WHERE status = 'online' ORDER BY name;


-- name: UpdateNode :one
UPDATE nodes
SET name = $2, endpoint = $3, grpc_endpoint = $4, region = $5, capacity_gbps = $6, status = $7, tags = $8, public_key = $9, cert_fingerprint = $10, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = $1;

-- name: UpdateNodeHeartbeat :exec
UPDATE nodes SET last_heartbeat = now(), status = $2, updated_at = now() WHERE id = $1;
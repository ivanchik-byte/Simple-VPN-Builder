-- name: UpsertTrafficStats :one
INSERT INTO traffic_stats (user_id, node_id, protocol, hour_bucket, rx_bytes, tx_bytes)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, node_id, protocol, hour_bucket) DO UPDATE SET
    rx_bytes = traffic_stats.rx_bytes + EXCLUDED.rx_bytes,
    tx_bytes = traffic_stats.tx_bytes + EXCLUDED.tx_bytes,
    created_at = now()
RETURNING *;

-- name: GetTrafficStatsByUserHour :one
SELECT * FROM traffic_stats WHERE user_id = $1 AND node_id = $2 AND protocol = $3 AND hour_bucket = $4;

-- name: ListTrafficStatsByUser :many
SELECT * FROM traffic_stats
WHERE user_id = $1
AND hour_bucket >= $2 AND hour_bucket <= $3
ORDER BY hour_bucket DESC;

-- name: GetTrafficAggregateByUser :one
SELECT
    COALESCE(SUM(rx_bytes), 0)::bigint as total_rx,
    COALESCE(SUM(tx_bytes), 0)::bigint as total_tx
FROM traffic_stats
WHERE user_id = $1 AND hour_bucket >= $2 AND hour_bucket <= $3;

-- name: GetTrafficAggregateByNode :many
SELECT
    node_id,
    protocol,
    COALESCE(SUM(rx_bytes), 0)::bigint as total_rx,
    COALESCE(SUM(tx_bytes), 0)::bigint as total_tx
FROM traffic_stats
WHERE hour_bucket >= $1 AND hour_bucket <= $2
GROUP BY node_id, protocol;
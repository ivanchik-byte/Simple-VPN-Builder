-- name: CreatePlan :one
INSERT INTO plans (name, monthly_price, traffic_limit, device_limit, protocols, features, is_active, is_trial, trial_duration_hours, price_stars, max_devices, traffic_limit_gb, price_1m, price_3m, price_6m, price_12m)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
RETURNING *;

-- name: GetPlanByID :one
SELECT * FROM plans WHERE id = $1;

-- name: GetPlanByName :one
SELECT * FROM plans WHERE name = $1;

-- name: ListPlans :many
SELECT * FROM plans WHERE is_active = true ORDER BY name;

-- name: UpdatePlan :one
UPDATE plans
SET name = $2, monthly_price = $3, traffic_limit = $4, device_limit = $5, protocols = $6, features = $7, is_active = $8, is_trial = $9, trial_duration_hours = $10, price_stars = $11, max_devices = $12, traffic_limit_gb = $13, price_1m = $14, price_3m = $15, price_6m = $16, price_12m = $17, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeletePlan :exec
DELETE FROM plans WHERE id = $1;

-- name: GetTrialPlan :one
SELECT * FROM plans WHERE is_trial = true AND is_active = true LIMIT 1;
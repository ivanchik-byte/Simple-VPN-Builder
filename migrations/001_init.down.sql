-- 001_init.down.sql
-- Rollback initial schema

DROP TRIGGER IF EXISTS update_webhooks_updated_at ON webhooks;
DROP TRIGGER IF EXISTS update_credentials_updated_at ON credentials;
DROP TRIGGER IF EXISTS update_plans_updated_at ON plans;
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
DROP TRIGGER IF EXISTS update_nodes_updated_at ON nodes;
DROP FUNCTION IF EXISTS update_updated_at_column();

DROP INDEX IF EXISTS idx_webhooks_active;
DROP INDEX IF EXISTS idx_api_keys_prefix;
DROP INDEX IF EXISTS idx_audit_logs_admin;
DROP INDEX IF EXISTS idx_audit_logs_created;
DROP INDEX IF EXISTS idx_nodes_status_region;
DROP INDEX IF EXISTS idx_traffic_stats_user_hour;
DROP INDEX IF EXISTS idx_credentials_user_node;
DROP INDEX IF EXISTS idx_users_subscription_token;
DROP INDEX IF EXISTS idx_users_status_expires;

DROP TABLE IF EXISTS webhooks;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS admins;
DROP TABLE IF EXISTS traffic_stats;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS plans;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS nodes;

DROP EXTENSION IF EXISTS "pgcrypto";
DROP EXTENSION IF EXISTS "uuid-ossp";
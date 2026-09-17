-- 011_node_config_version.up.sql
-- Monotonic per-node config version for agent resync (N2).
-- Agents drop stale updates (ConfigVersion <= current), so every
-- PushConfigUpdate must carry a strictly increasing version.
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS config_version BIGINT NOT NULL DEFAULT 1;

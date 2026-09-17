-- 011_node_config_version.down.sql
ALTER TABLE nodes DROP COLUMN IF EXISTS config_version;

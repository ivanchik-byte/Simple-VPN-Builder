-- 012_credential_unique_ip.up.sql
-- Safety net for concurrent IP allocation races (N6).
-- NOTE: PostgreSQL has no partial UNIQUE *constraint* syntax, so a partial
-- UNIQUE INDEX is used instead: at most one credential may hold a given
-- IPv4 on a node; NULL ipv4 (e.g. vless) is unaffected.
CREATE UNIQUE INDEX IF NOT EXISTS credentials_node_ipv4_unique
	ON credentials (node_id, ipv4) WHERE ipv4 IS NOT NULL;

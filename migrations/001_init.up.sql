-- 001_init.up.sql
-- Initial schema for Simple-VPN-Builder

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Nodes (VPN exit servers)
CREATE TABLE nodes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL UNIQUE,
    endpoint            VARCHAR(255) NOT NULL,
    grpc_endpoint       VARCHAR(255) NOT NULL,
    region              VARCHAR(64),
    capacity_gbps       INT DEFAULT 1,
    status              VARCHAR(32) DEFAULT 'pending',
    tags                JSONB DEFAULT '{}',
    public_key          VARCHAR(88) NOT NULL,
    cert_fingerprint    VARCHAR(64) NOT NULL,
    last_heartbeat      TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now()
);

-- Users (VPN customers)
CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               VARCHAR(255) UNIQUE,
    username            VARCHAR(128) UNIQUE NOT NULL,
    password_hash       VARCHAR(255),
    status              VARCHAR(32) DEFAULT 'active',
    plan_id             UUID,
    traffic_limit       BIGINT DEFAULT 0,
    traffic_used        BIGINT DEFAULT 0,
    expires_at          TIMESTAMPTZ,
    note                TEXT,
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now()
);

-- Subscription Plans
CREATE TABLE plans (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(64) NOT NULL UNIQUE,
    monthly_price       DECIMAL(10,2) DEFAULT 0,
    traffic_limit       BIGINT DEFAULT 0,
    device_limit        INT DEFAULT 3,
    protocols           TEXT[] DEFAULT ARRAY['wireguard','vless'],
    features            JSONB DEFAULT '{}',
    is_active           BOOLEAN DEFAULT true,
    created_at          TIMESTAMPTZ DEFAULT now()
);

-- Protocol-specific credentials (per user per node)
CREATE TABLE credentials (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id             UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    protocol            VARCHAR(32) NOT NULL,
    private_key         VARCHAR(88),
    public_key          VARCHAR(88),
    preshared_key       VARCHAR(88),
    uuid                UUID,
    password            VARCHAR(128),
    email               VARCHAR(255),
    flow                VARCHAR(64),
    ipv4                INET,
    ipv6                INET,
    dns                 VARCHAR(255) DEFAULT '1.1.1.1',
    mtu                 INT DEFAULT 1280,
    keepalive           INT DEFAULT 25,
    allowed_ips         CIDR[] DEFAULT ARRAY['0.0.0.0/0','::/0'],
    status              VARCHAR(32) DEFAULT 'active',
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now(),
    UNIQUE (user_id, node_id, protocol)
);

-- Traffic accounting (hourly rollups)
CREATE TABLE traffic_stats (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id             UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    protocol            VARCHAR(32) NOT NULL,
    hour_bucket         TIMESTAMPTZ NOT NULL,
    rx_bytes            BIGINT DEFAULT 0,
    tx_bytes            BIGINT DEFAULT 0,
    created_at          TIMESTAMPTZ DEFAULT now(),
    UNIQUE (user_id, node_id, protocol, hour_bucket)
);

-- Admin users (control plane access)
CREATE TABLE admins (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               VARCHAR(255) NOT NULL UNIQUE,
    password_hash       VARCHAR(255) NOT NULL,
    role                VARCHAR(32) DEFAULT 'admin',
    totp_secret         VARCHAR(32),
    last_login          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT now()
);

-- API Keys (for integrations/Terraform)
CREATE TABLE api_keys (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL,
    key_hash            VARCHAR(64) NOT NULL UNIQUE,
    prefix              VARCHAR(16) NOT NULL,
    scopes              TEXT[] DEFAULT ARRAY['read'],
    expires_at          TIMESTAMPTZ,
    last_used_at        TIMESTAMPTZ,
    created_by          UUID REFERENCES admins(id),
    created_at          TIMESTAMPTZ DEFAULT now()
);

-- Audit log
CREATE TABLE audit_logs (
    id                  BIGSERIAL PRIMARY KEY,
    admin_id            UUID REFERENCES admins(id),
    api_key_id          UUID REFERENCES api_keys(id),
    action              VARCHAR(64) NOT NULL,
    resource_type       VARCHAR(64),
    resource_id         UUID,
    diff                JSONB,
    ip_address          INET,
    user_agent          TEXT,
    created_at          TIMESTAMPTZ DEFAULT now()
);

-- Indexes
CREATE INDEX idx_users_status_expires ON users(status, expires_at);
CREATE INDEX idx_credentials_user_node ON credentials(user_id, node_id);
CREATE INDEX idx_traffic_stats_user_hour ON traffic_stats(user_id, hour_bucket DESC);
CREATE INDEX idx_nodes_status_region ON nodes(status, region);
CREATE INDEX idx_audit_logs_created ON audit_logs(created_at DESC);
CREATE INDEX idx_audit_logs_admin ON audit_logs(admin_id, created_at DESC);
CREATE INDEX idx_api_keys_prefix ON api_keys(prefix);

-- Updated_at triggers
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_nodes_updated_at BEFORE UPDATE ON nodes FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_plans_updated_at BEFORE UPDATE ON plans FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_credentials_updated_at BEFORE UPDATE ON credentials FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
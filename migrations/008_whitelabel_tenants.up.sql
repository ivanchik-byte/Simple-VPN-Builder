-- 008_whitelabel_tenants.up.sql
-- Multi-Brand / White-Label partners, custom Telegram bots, channels, and mini-apps

CREATE TABLE IF NOT EXISTS tenants (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id            UUID REFERENCES admins(id) ON DELETE SET NULL,
    name                VARCHAR(128) NOT NULL,
    slug                VARCHAR(64) NOT NULL UNIQUE,
    bot_token           VARCHAR(255) NOT NULL,
    bot_username        VARCHAR(128),
    bot_webhook_secret  VARCHAR(64) NOT NULL DEFAULT encode(gen_random_bytes(24), 'hex'),
    channel_link        VARCHAR(255),
    channel_id          BIGINT,
    require_channel_sub BOOLEAN NOT NULL DEFAULT false,
    support_link        VARCHAR(255),
    custom_domain       VARCHAR(255),
    miniapp_url         VARCHAR(255),
    banner_url          TEXT,
    welcome_text        TEXT,
    is_active           BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tenants_admin_id ON tenants(admin_id);
CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
CREATE INDEX IF NOT EXISTS idx_tenants_is_active ON tenants(is_active);

-- Link users to specific partner tenant
ALTER TABLE users ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id);

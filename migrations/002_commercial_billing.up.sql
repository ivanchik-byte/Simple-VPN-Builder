-- 002_commercial_billing.up.sql
-- Commercial billing, Telegram identity, orders, promo codes, and broadcast campaigns.

-- 1. Extend Users with Telegram attributes, trial flags, and referral links
ALTER TABLE users ADD COLUMN IF NOT EXISTS telegram_id BIGINT UNIQUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS telegram_username VARCHAR(64);
ALTER TABLE users ADD COLUMN IF NOT EXISTS trial_used BOOLEAN DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS referrer_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS referral_code VARCHAR(32) UNIQUE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_banned BOOLEAN DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS ban_reason TEXT;

CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id);
CREATE INDEX IF NOT EXISTS idx_users_referral_code ON users(referral_code);

-- 2. Extend Plans with trial parameters and Stars pricing
ALTER TABLE plans ADD COLUMN IF NOT EXISTS is_trial BOOLEAN DEFAULT false;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS trial_duration_hours INT DEFAULT 24;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS price_stars INT DEFAULT 0;

-- 3. Dynamic Payment Gateways configuration (managed via Web Admin)
CREATE TABLE IF NOT EXISTS payment_gateways (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(32) NOT NULL UNIQUE,
    is_enabled          BOOLEAN DEFAULT false,
    config_encrypted    TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now()
);

-- 4. Commercial Orders & Invoices tracking
CREATE TABLE IF NOT EXISTS orders (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id             UUID NOT NULL REFERENCES plans(id),
    gateway             VARCHAR(32) NOT NULL,
    external_invoice_id VARCHAR(128),
    amount              DECIMAL(10,2) NOT NULL,
    currency            VARCHAR(8) NOT NULL,
    status              VARCHAR(32) DEFAULT 'pending',
    duration_months     INT DEFAULT 1,
    metadata            JSONB DEFAULT '{}',
    paid_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_external_invoice_id ON orders(external_invoice_id);

-- 5. Promotional Discount Codes
CREATE TABLE IF NOT EXISTS promo_codes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code                VARCHAR(32) NOT NULL UNIQUE,
    discount_percent    INT DEFAULT 0,
    discount_amount     DECIMAL(10,2) DEFAULT 0,
    bonus_days          INT DEFAULT 0,
    bonus_bytes         BIGINT DEFAULT 0,
    max_uses            INT DEFAULT 0,
    used_count          INT DEFAULT 0,
    is_active           BOOLEAN DEFAULT true,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_promo_codes_code ON promo_codes(code);

-- 6. Segmented Broadcast Campaigns
CREATE TABLE IF NOT EXISTS broadcast_campaigns (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title               VARCHAR(128) NOT NULL,
    target_segment      VARCHAR(32) NOT NULL,
    message_text        TEXT NOT NULL,
    inline_buttons      JSONB DEFAULT '[]',
    total_recipients    INT DEFAULT 0,
    sent_count          INT DEFAULT 0,
    failed_count        INT DEFAULT 0,
    status              VARCHAR(32) DEFAULT 'draft',
    created_at          TIMESTAMPTZ DEFAULT now(),
    completed_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_broadcast_campaigns_status ON broadcast_campaigns(status);

-- Triggers for updated_at
CREATE TRIGGER update_payment_gateways_updated_at BEFORE UPDATE ON payment_gateways FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
CREATE TRIGGER update_orders_updated_at BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

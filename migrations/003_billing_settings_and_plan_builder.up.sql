-- 003_billing_settings_and_plan_builder.up.sql
-- Billing gateway settings and flexible plan builder extensions.

CREATE TABLE IF NOT EXISTS billing_settings (
    id                     INT PRIMARY KEY DEFAULT 1,
    cryptobot_api_token    TEXT NOT NULL DEFAULT '',
    cryptobot_enabled      BOOLEAN NOT NULL DEFAULT false,
    telegram_stars_enabled BOOLEAN NOT NULL DEFAULT true,
    stars_price_per_month  INT NOT NULL DEFAULT 250,
    webhook_secret         TEXT NOT NULL DEFAULT '',
    updated_at             TIMESTAMPTZ DEFAULT now(),
    CONSTRAINT single_billing_settings CHECK (id = 1)
);

INSERT INTO billing_settings (id, cryptobot_api_token, cryptobot_enabled, telegram_stars_enabled, stars_price_per_month, webhook_secret)
VALUES (1, '', false, true, 250, '')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE plans ADD COLUMN IF NOT EXISTS max_devices INT DEFAULT 3;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS traffic_limit_gb INT DEFAULT 0;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS price_1m DECIMAL(10,2) DEFAULT 0;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS price_3m DECIMAL(10,2) DEFAULT 0;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS price_6m DECIMAL(10,2) DEFAULT 0;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS price_12m DECIMAL(10,2) DEFAULT 0;

UPDATE plans SET max_devices = COALESCE(device_limit, 3) WHERE max_devices IS NULL;
UPDATE plans SET price_1m = COALESCE(monthly_price, 0) WHERE price_1m IS NULL OR price_1m = 0;
UPDATE plans SET traffic_limit_gb = COALESCE(traffic_limit / (1024*1024*1024), 0) WHERE traffic_limit_gb IS NULL OR traffic_limit_gb = 0;

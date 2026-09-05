-- 003_billing_settings_and_plan_builder.down.sql

DROP TABLE IF EXISTS billing_settings;
ALTER TABLE plans DROP COLUMN IF EXISTS max_devices;
ALTER TABLE plans DROP COLUMN IF EXISTS traffic_limit_gb;
ALTER TABLE plans DROP COLUMN IF EXISTS price_1m;
ALTER TABLE plans DROP COLUMN IF EXISTS price_3m;
ALTER TABLE plans DROP COLUMN IF EXISTS price_6m;
ALTER TABLE plans DROP COLUMN IF EXISTS price_12m;

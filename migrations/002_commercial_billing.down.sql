-- 002_commercial_billing.down.sql
DROP TABLE IF EXISTS broadcast_campaigns CASCADE;
DROP TABLE IF EXISTS promo_codes CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS payment_gateways CASCADE;

ALTER TABLE plans DROP COLUMN IF EXISTS price_stars;
ALTER TABLE plans DROP COLUMN IF EXISTS trial_duration_hours;
ALTER TABLE plans DROP COLUMN IF EXISTS is_trial;

DROP INDEX IF EXISTS idx_users_referral_code;
DROP INDEX IF EXISTS idx_users_telegram_id;

ALTER TABLE users DROP COLUMN IF EXISTS ban_reason;
ALTER TABLE users DROP COLUMN IF EXISTS is_banned;
ALTER TABLE users DROP COLUMN IF EXISTS referral_code;
ALTER TABLE users DROP COLUMN IF EXISTS referrer_id;
ALTER TABLE users DROP COLUMN IF EXISTS trial_used;
ALTER TABLE users DROP COLUMN IF EXISTS telegram_username;
ALTER TABLE users DROP COLUMN IF EXISTS telegram_id;

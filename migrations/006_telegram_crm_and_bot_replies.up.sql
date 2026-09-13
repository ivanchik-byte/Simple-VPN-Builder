-- 006_telegram_crm_and_bot_replies.up.sql
-- CRM lead tracking on /start and customizable bot replies

ALTER TABLE users 
    ADD COLUMN IF NOT EXISTS telegram_first_name   VARCHAR(128),
    ADD COLUMN IF NOT EXISTS telegram_last_name    VARCHAR(128),
    ADD COLUMN IF NOT EXISTS telegram_language_code VARCHAR(16) DEFAULT 'en',
    ADD COLUMN IF NOT EXISTS last_seen_at          TIMESTAMPTZ DEFAULT now(),
    ADD COLUMN IF NOT EXISTS is_bot_blocked        BOOLEAN DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_users_last_seen_at ON users(last_seen_at);
CREATE INDEX IF NOT EXISTS idx_users_status_tg ON users(status, telegram_id) WHERE telegram_id IS NOT NULL;

-- Bot replies customization table
CREATE TABLE IF NOT EXISTS bot_replies (
    key_name    VARCHAR(64) PRIMARY KEY,
    reply_text  TEXT NOT NULL,
    updated_at  TIMESTAMPTZ DEFAULT now()
);

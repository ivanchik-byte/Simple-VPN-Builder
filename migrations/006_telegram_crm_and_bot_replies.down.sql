DROP TABLE IF EXISTS bot_replies;
DROP INDEX IF EXISTS idx_users_status_tg;
DROP INDEX IF EXISTS idx_users_last_seen_at;
ALTER TABLE users 
    DROP COLUMN IF EXISTS is_bot_blocked,
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS telegram_language_code,
    DROP COLUMN IF EXISTS telegram_last_name,
    DROP COLUMN IF EXISTS telegram_first_name;

-- 007_email_policy_and_otp.up.sql
-- Email verification codes and OTP-based account recovery

CREATE TABLE IF NOT EXISTS user_email_verifications (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    telegram_id        BIGINT NOT NULL,
    email              VARCHAR(255) NOT NULL,
    otp_hash           VARCHAR(64) NOT NULL,
    purpose            VARCHAR(32) NOT NULL DEFAULT 'link_email', -- 'link_email' or 'restore_account'
    attempts_remaining INT NOT NULL DEFAULT 3,
    expires_at         TIMESTAMPTZ NOT NULL,
    verified_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_email_verifications_active 
ON user_email_verifications (email, telegram_id, expires_at) 
WHERE verified_at IS NULL;

-- 005_audit_logs_hardening.up.sql
-- Enforce write-once-read-many (WORM) immutability on audit_logs table

CREATE OR REPLACE FUNCTION prevent_audit_log_tampering()
RETURNS TRIGGER AS $$
BEGIN
    -- Allow maintenance / test tear-down if explicitly enabled in session
    IF current_setting('audit.allow_maintenance', true) = 'on' THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        ELSIF TG_OP = 'UPDATE' THEN
            RETURN NEW;
        END IF;
        RETURN NULL;
    END IF;

    RAISE EXCEPTION 'Audit logs are immutable. UPDATE, DELETE, and TRUNCATE operations are denied.'
        USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_audit_logs_immutable_row ON audit_logs;
CREATE TRIGGER trg_audit_logs_immutable_row
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW
    EXECUTE FUNCTION prevent_audit_log_tampering();

DROP TRIGGER IF EXISTS trg_audit_logs_immutable_stmt ON audit_logs;
CREATE TRIGGER trg_audit_logs_immutable_stmt
    BEFORE TRUNCATE ON audit_logs
    FOR EACH STATEMENT
    EXECUTE FUNCTION prevent_audit_log_tampering();

-- 005_audit_logs_hardening.down.sql

DROP TRIGGER IF EXISTS trg_audit_logs_immutable_stmt ON audit_logs;
DROP TRIGGER IF EXISTS trg_audit_logs_immutable_row ON audit_logs;
DROP FUNCTION IF EXISTS prevent_audit_log_tampering();

-- 013_users_plan_fk.up.sql
-- Enforce users.plan_id -> plans(id) without blocking writes on large tables:
-- add the constraint NOT VALID, then validate it as a separate statement.
ALTER TABLE users ADD CONSTRAINT fk_users_plan FOREIGN KEY (plan_id) REFERENCES plans(id) NOT VALID;
ALTER TABLE users VALIDATE CONSTRAINT fk_users_plan;

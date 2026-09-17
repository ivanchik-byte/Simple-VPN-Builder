-- 013_users_plan_fk.down.sql
ALTER TABLE users DROP CONSTRAINT IF EXISTS fk_users_plan;

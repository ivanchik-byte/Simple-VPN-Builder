-- 004_admin_permissions.down.sql
ALTER TABLE admins DROP COLUMN IF EXISTS permissions;

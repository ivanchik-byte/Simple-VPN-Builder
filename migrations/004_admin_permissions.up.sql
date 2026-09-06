-- 004_admin_permissions.up.sql
-- Granular RBAC permissions for administrators to protect Telegram broadcast and nodes

ALTER TABLE admins ADD COLUMN IF NOT EXISTS permissions JSONB NOT NULL DEFAULT '{
  "can_broadcast": false,
  "can_manage_users": true,
  "can_delete_users": false,
  "can_reset_traffic": true,
  "can_manage_nodes": false,
  "can_manage_plans": false,
  "can_view_audit": false
}'::jsonb;

-- Owners and superadmins grant all permissions by default
UPDATE admins SET permissions = '{
  "can_broadcast": true,
  "can_manage_users": true,
  "can_delete_users": true,
  "can_reset_traffic": true,
  "can_manage_nodes": true,
  "can_manage_plans": true,
  "can_view_audit": true
}'::jsonb WHERE role IN ('owner', 'superadmin');

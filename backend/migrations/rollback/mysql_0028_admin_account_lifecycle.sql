ALTER TABLE users DROP COLUMN account_status;
DELETE FROM schema_migrations WHERE name='mysql/0028_admin_account_lifecycle.sql';

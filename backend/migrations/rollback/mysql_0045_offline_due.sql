ALTER TABLE device DROP INDEX idx_device_offline_due, DROP COLUMN offline_due_at;
DELETE FROM schema_migrations WHERE name='mysql/0045_offline_due.sql';

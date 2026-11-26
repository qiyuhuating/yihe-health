-- Removed replication triggers must be restored from the pre-upgrade SQL backup.
-- Do not recreate them against an incompatible release or discard outbox rows.
SELECT name FROM schema_migrations WHERE name='mysql/0034_retire_pg_projection.sql';

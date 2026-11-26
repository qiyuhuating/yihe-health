-- Stop the security alert dispatcher before rolling back the application
-- Retain audit evidence, append-only triggers and pending alerts for recovery
-- Keep the ledger entry because this rollback retains every schema object.
-- Removing it would ask the migrator to recreate existing append-only triggers.
SELECT name FROM schema_migrations WHERE name='mysql/0029_supervisor_recovery_audit.sql';

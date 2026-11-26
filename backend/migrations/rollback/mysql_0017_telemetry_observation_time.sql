-- Retain observation timestamps and indexes for safe reapplication.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0017_telemetry_observation_time.sql';

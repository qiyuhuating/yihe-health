-- Offline rollback preserves encrypted pending and completed telemetry records.
-- A previous server may ignore this additive table, and reapplying migration
-- preserves the records and unique idempotence keys.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0016_durable_telemetry_inbox.sql';

-- Preserve event metadata when reverting application code.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0020_position_metadata.sql';

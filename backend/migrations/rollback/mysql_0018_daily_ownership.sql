-- Keep all ownership and archived snapshots when reverting application code.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0018_daily_ownership.sql';

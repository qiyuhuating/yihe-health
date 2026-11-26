-- Keep all sleep observations and archived snapshots when reverting application code.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0019_sleep_observation_time.sql';

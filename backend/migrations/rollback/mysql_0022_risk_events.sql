-- Offline rollback keeps IP rules and login audit events when reverting the
-- application version. Reapplying the migration uses CREATE TABLE IF NOT
-- EXISTS and leaves both tables intact.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0022_risk_events.sql';

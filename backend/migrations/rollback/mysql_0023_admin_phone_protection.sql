-- Offline rollback keeps the protected columns, lookup index and key state so
-- encrypted administrator phone numbers are never destroyed. Reapplying the
-- additive migration tolerates duplicate columns and indexes.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0023_admin_phone_protection.sql';

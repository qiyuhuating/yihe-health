-- Keep encrypted data, lookup indexes and the migration ledger. An older
-- application cannot read protected phones. Restore a tested pre-migration
-- database together with its matching application instead of dropping columns.
SELECT name FROM schema_migrations WHERE name='mysql/0031_family_phone_protection.sql';

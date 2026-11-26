-- Retain encrypted dates. Restore a matching backup before running an older application.
SELECT name FROM schema_migrations WHERE name='mysql/0040_resident_birthdate_protection.sql';

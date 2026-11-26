-- Retain protected addresses. Restore a matching backup before running an older application.
SELECT name FROM schema_migrations WHERE name='mysql/0038_family_address_protection.sql';

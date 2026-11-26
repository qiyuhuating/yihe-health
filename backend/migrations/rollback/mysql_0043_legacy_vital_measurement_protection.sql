-- Retain protected values. Restore a matching backup before running an older application.
SELECT name FROM schema_migrations WHERE name='mysql/0043_legacy_vital_measurement_protection.sql';

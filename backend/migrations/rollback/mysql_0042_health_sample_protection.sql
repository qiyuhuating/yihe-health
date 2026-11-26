-- Retain protected samples. Restore a matching backup before running an older application.
SELECT name FROM schema_migrations WHERE name='mysql/0042_health_sample_protection.sql';

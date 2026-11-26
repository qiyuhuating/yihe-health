-- Retain protected payloads. Restore a matching backup before running an older application.
SELECT name FROM schema_migrations WHERE name='mysql/0041_health_projection_protection.sql';

-- Do not rewind allocated revision numbers. Reserved gaps remain safe.
-- This migration does not change the schema.
DELETE FROM schema_migrations WHERE name='mysql/0051_existing_owner_ranges.sql';

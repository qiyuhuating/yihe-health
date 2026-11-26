DROP TABLE IF EXISTS care_duty_migration_inventory;
DELETE FROM schema_migrations WHERE name='mysql/0027_explicit_care_authority.sql';

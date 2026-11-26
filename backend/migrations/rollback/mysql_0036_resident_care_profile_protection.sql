-- Retain protected values, indexes and key binding. Stop relevant writers and
-- restore a tested matching application/database backup before an old binary
-- is deployed. This marker never restores plaintext or drops protected data.
SELECT name FROM schema_migrations WHERE name='mysql/0036_resident_care_profile_protection.sql';

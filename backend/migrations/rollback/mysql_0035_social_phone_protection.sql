-- Retain the expansion columns and key binding on rollback.
-- Older binaries cannot read protected identities. Stop social writers and
-- restore a verified backup with its matching retained keys before deployment.
-- This marker deliberately performs no plaintext downgrade or column removal.
SELECT name FROM schema_migrations WHERE name='mysql/0035_social_phone_protection.sql';

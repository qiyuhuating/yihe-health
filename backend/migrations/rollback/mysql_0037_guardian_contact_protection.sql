-- Retain contacts and their reader/binding marker. Stop relevant writers and
-- restore a tested matching backup before deploying an older application.
-- Never discard protected contacts or automatically return them to plaintext.
SELECT name FROM schema_migrations WHERE name='mysql/0037_guardian_contact_protection.sql';

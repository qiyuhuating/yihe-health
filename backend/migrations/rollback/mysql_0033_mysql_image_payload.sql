-- Data-bearing MySQL image migration has no safe in-place rollback.
-- Restore the full pre-upgrade MySQL snapshot and original image backup.
SELECT name FROM schema_migrations WHERE name='mysql/0033_mysql_image_payload.sql';

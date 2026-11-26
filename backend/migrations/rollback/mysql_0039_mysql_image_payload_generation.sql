-- Preserve generation pointers and ciphertext. Restore a matching backup for older application versions.
SELECT name FROM schema_migrations WHERE name='mysql/0039_mysql_image_payload_generation.sql';

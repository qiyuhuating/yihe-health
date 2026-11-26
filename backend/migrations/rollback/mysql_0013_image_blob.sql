-- Preserve catalog/tombstones and image bytes, never erase live image history.
-- Old binaries cannot read S3 images: materialize local images before rollback.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0013_image_blob.sql';

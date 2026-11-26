-- Stop and drain all application instances before restoring textual identities.
ALTER TABLE health_sample_latest ADD COLUMN sample_hex CHAR(64) NULL;
UPDATE health_sample_latest SET sample_hex=LOWER(HEX(sample_id)) WHERE sample_hex IS NULL;
ALTER TABLE health_sample_latest DROP COLUMN sample_id,
 CHANGE COLUMN sample_hex sample_id CHAR(64) NOT NULL AFTER series_key;

ALTER TABLE health_change ADD COLUMN sample_hex CHAR(64) NULL;
UPDATE health_change SET sample_hex=LOWER(HEX(sample_id)) WHERE sample_hex IS NULL;
ALTER TABLE health_change DROP COLUMN sample_id,
 CHANGE COLUMN sample_hex sample_id CHAR(64) NOT NULL AFTER old_id;

ALTER TABLE health_sample ADD COLUMN sample_hex CHAR(64) NULL;
UPDATE health_sample SET sample_hex=LOWER(HEX(sample_id)) WHERE sample_hex IS NULL;
ALTER TABLE health_sample DROP INDEX idx_health_sample_encryption, DROP PRIMARY KEY, DROP COLUMN sample_id,
 CHANGE COLUMN sample_hex sample_id CHAR(64) NOT NULL FIRST, ADD PRIMARY KEY(sample_id),
 ADD INDEX idx_health_sample_encryption(payload_encrypted,sample_id);
DELETE FROM schema_migrations WHERE name='mysql/0048_health_binary_identity.sql';

-- Stop and drain writers and readers before migrating physical identities.
-- Fail before conversion if any legacy identity is not canonical SHA-256 hex.
ALTER TABLE health_sample ADD COLUMN sample_key BINARY(32) NULL,
 ADD CONSTRAINT ck_health_sample_hex CHECK (UNHEX(sample_id) IS NOT NULL AND BINARY sample_id=BINARY LOWER(HEX(UNHEX(sample_id))) AND CHAR_LENGTH(sample_id)=64);
UPDATE health_sample SET sample_key=UNHEX(sample_id) WHERE sample_key IS NULL;
ALTER TABLE health_sample DROP CHECK ck_health_sample_hex, DROP INDEX idx_health_sample_encryption, DROP PRIMARY KEY,
 DROP COLUMN sample_id, CHANGE COLUMN sample_key sample_id BINARY(32) NOT NULL FIRST,
 ADD PRIMARY KEY(sample_id), ADD INDEX idx_health_sample_encryption(payload_encrypted,sample_id);

ALTER TABLE health_change ADD COLUMN sample_key BINARY(32) NULL,
 ADD CONSTRAINT ck_health_change_hex CHECK (UNHEX(sample_id) IS NOT NULL AND BINARY sample_id=BINARY LOWER(HEX(UNHEX(sample_id))) AND CHAR_LENGTH(sample_id)=64);
UPDATE health_change SET sample_key=UNHEX(sample_id) WHERE sample_key IS NULL;
ALTER TABLE health_change DROP CHECK ck_health_change_hex, DROP COLUMN sample_id,
 CHANGE COLUMN sample_key sample_id BINARY(32) NOT NULL AFTER old_id;

ALTER TABLE health_sample_latest ADD COLUMN sample_key BINARY(32) NULL,
 ADD CONSTRAINT ck_health_latest_hex CHECK (UNHEX(sample_id) IS NOT NULL AND BINARY sample_id=BINARY LOWER(HEX(UNHEX(sample_id))) AND CHAR_LENGTH(sample_id)=64);
UPDATE health_sample_latest SET sample_key=UNHEX(sample_id) WHERE sample_key IS NULL;
ALTER TABLE health_sample_latest DROP CHECK ck_health_latest_hex, DROP COLUMN sample_id,
 CHANGE COLUMN sample_key sample_id BINARY(32) NOT NULL AFTER series_key;

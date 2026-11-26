-- Preserve the covering query for historical samples. Protected rows retain
-- indexed routing metadata and carry no value in the generated rule columns.
ALTER TABLE health_sample
 MODIFY payload JSON NULL,
 ADD COLUMN payload_cipher MEDIUMTEXT DEFAULT NULL,
 ADD COLUMN payload_encrypted BOOLEAN GENERATED ALWAYS AS (payload_cipher IS NOT NULL) VIRTUAL,
 ADD CONSTRAINT ck_health_sample_payload CHECK ((payload IS NULL) <> (payload_cipher IS NULL)),
 DROP INDEX idx_health_rule_cover,
 ADD INDEX idx_health_rule_cover(old_id,device_id,metric,deleted,payload_encrypted,source,rule_source_device,sample_start,rule_value,rule_legacy_unzoned),
 ADD INDEX idx_health_sample_encryption(payload_encrypted,sample_id);

-- Quiesce application instances before restoring prefix routing.
ALTER TABLE health_sample
 DROP INDEX idx_health_rule_cover,
 DROP COLUMN rule_identity_key,
 ADD COLUMN rule_source_key VARBINARY(65) GENERATED ALWAYS AS
   (CONCAT(IF(OCTET_LENGTH(source_device)<=64,0x00,0x01),LEFT(CAST(source_device AS BINARY),64))) VIRTUAL AFTER deleted,
 ADD INDEX idx_health_rule_cover(old_id,device_id,metric,deleted,payload_encrypted,source,rule_source_key,sample_start,rule_value,rule_legacy_unzoned);
DELETE FROM schema_migrations WHERE name='mysql/0049_health_rule_identity_hash.sql';

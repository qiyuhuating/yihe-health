-- Quiesce application instances before replacing the baseline routing key.
-- Hash only long identities and retain exact short keys and full long equality.
ALTER TABLE health_sample
 DROP INDEX idx_health_rule_cover,
 DROP COLUMN rule_source_key,
 ADD COLUMN rule_identity_key VARBINARY(65) GENERATED ALWAYS AS
   (CASE WHEN OCTET_LENGTH(source_device)<=64 THEN CONCAT(0x00,CAST(source_device AS BINARY)) ELSE CONCAT(0x01,UNHEX(SHA2(CAST(source_device AS BINARY),256))) END) VIRTUAL AFTER deleted,
 ADD INDEX idx_health_rule_cover(old_id,device_id,metric,deleted,payload_encrypted,source,rule_identity_key,sample_start,rule_value,rule_legacy_unzoned);

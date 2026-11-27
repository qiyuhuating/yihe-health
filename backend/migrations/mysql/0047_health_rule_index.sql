-- Quiesce application instances before replacing the baseline routing index.
-- Tag the byte prefix so a short identity cannot match a longer one with
-- the same first 64 bytes. Long queries still compare the full identity.
ALTER TABLE health_sample
 DROP INDEX idx_health_rule_source,
 DROP INDEX idx_health_rule_cover,
 DROP COLUMN rule_source_device,
 ADD COLUMN rule_source_key VARBINARY(65) GENERATED ALWAYS AS
   (CONCAT(IF(OCTET_LENGTH(source_device)<=64,0x00,0x01),LEFT(CAST(source_device AS BINARY),64))) VIRTUAL AFTER deleted,
 ADD INDEX idx_health_rule_cover(old_id,device_id,metric,deleted,payload_encrypted,source,rule_source_key,sample_start,rule_value,rule_legacy_unzoned);

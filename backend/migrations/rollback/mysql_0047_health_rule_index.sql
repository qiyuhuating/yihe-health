-- Quiesce application instances before restoring the previous baseline indexes.
ALTER TABLE health_sample
 DROP INDEX idx_health_rule_cover,
 DROP COLUMN rule_source_key,
 ADD COLUMN rule_source_device varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin GENERATED ALWAYS AS (CASE WHEN CHAR_LENGTH(source_device)<=64 THEN source_device ELSE NULL END) VIRTUAL AFTER deleted,
 ADD INDEX idx_health_rule_source(old_id,device_id,metric,deleted,source(64),source_device(64),sample_start),
 ADD INDEX idx_health_rule_cover(old_id,device_id,metric,deleted,payload_encrypted,source,rule_source_device,sample_start,rule_value,rule_legacy_unzoned);
DELETE FROM schema_migrations WHERE name='mysql/0047_health_rule_index.sql';

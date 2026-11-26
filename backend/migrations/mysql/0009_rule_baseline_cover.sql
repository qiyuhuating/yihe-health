-- Virtual values are materialized only in the secondary index, not copied into
-- every clustered payload row. Long source-device identities keep the fallback.
ALTER TABLE health_sample ADD COLUMN rule_source_device varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin GENERATED ALWAYS AS (CASE WHEN CHAR_LENGTH(source_device)<=64 THEN source_device ELSE NULL END) VIRTUAL;
ALTER TABLE health_sample ADD COLUMN rule_value double GENERATED ALWAYS AS (CASE WHEN JSON_TYPE(JSON_EXTRACT(payload,'$.value')) IN ('INTEGER','DOUBLE','DECIMAL') THEN CAST(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.value')) AS DOUBLE) ELSE NULL END) VIRTUAL;
ALTER TABLE health_sample ADD COLUMN rule_legacy_unzoned boolean GENERATED ALWAYS AS (CASE WHEN JSON_UNQUOTE(JSON_EXTRACT(payload,'$.timeBasis'))='legacy-unzoned' THEN true ELSE false END) VIRTUAL;
CREATE INDEX idx_health_rule_cover ON health_sample(old_id,device_id,metric,deleted,source,rule_source_device,sample_start,rule_value,rule_legacy_unzoned);

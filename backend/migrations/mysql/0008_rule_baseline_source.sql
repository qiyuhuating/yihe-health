-- Bound baseline reads while the rule transaction holds the device row lock.
-- Prefixes narrow candidates, while full binary source checks remain in SQL.
CREATE INDEX idx_health_rule_source ON health_sample(old_id,device_id,metric,deleted,source(64),source_device(64),sample_start);

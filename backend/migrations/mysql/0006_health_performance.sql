-- Query support only. No data, ownership, source or rule-window changes.
CREATE INDEX idx_health_rule_window ON health_sample(old_id,device_id,metric,deleted,sample_start,revision);

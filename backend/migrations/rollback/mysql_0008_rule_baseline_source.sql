-- Stop application workers before rolling back query support.
DROP INDEX idx_health_rule_source ON health_sample;
DELETE FROM schema_migrations WHERE name='mysql/0008_rule_baseline_source.sql';

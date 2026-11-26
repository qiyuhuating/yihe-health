-- Stop application writers and back up before rolling back.
DROP INDEX idx_health_rule_window ON health_sample;
DELETE FROM schema_migrations WHERE name='mysql/0006_health_performance.sql';

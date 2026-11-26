-- Block incompatible encrypted-schema rollback before any destructive DDL.
-- Re-inserting the existing successor ledger row fails with duplicate key 1062.
INSERT INTO schema_migrations SELECT * FROM schema_migrations WHERE name='mysql/0041_health_projection_protection.sql';
-- Latest health snapshots are derived from health_sample and can be rebuilt.
DROP INDEX idx_health_latest_recompute ON health_sample;
DROP TABLE IF EXISTS health_latest_ready;
DROP TABLE IF EXISTS health_sample_latest;
DELETE FROM schema_migrations WHERE name='mysql/0024_health_latest.sql';

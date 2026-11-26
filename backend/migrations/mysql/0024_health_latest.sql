CREATE TABLE health_sample_latest (
 old_id bigint NOT NULL,
 series_key binary(32) NOT NULL,
 sample_id char(64) NOT NULL,
 device_id integer NULL,
 epoch bigint NOT NULL,
 revision bigint NOT NULL,
 payload json NOT NULL,
 PRIMARY KEY(old_id,series_key)
) ENGINE=InnoDB;
CREATE TABLE health_latest_ready (
 old_id bigint PRIMARY KEY
) ENGINE=InnoDB;
CREATE INDEX idx_health_latest_recompute ON health_sample(old_id,source(64),source_device(64),metric,semantics,sample_start);

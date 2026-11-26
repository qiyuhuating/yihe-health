ALTER TABLE device ADD COLUMN health_epoch bigint NOT NULL DEFAULT 1;
CREATE TRIGGER device_health_epoch BEFORE UPDATE ON device FOR EACH ROW SET NEW.health_epoch=IF(NOT (NEW.old_id <=> OLD.old_id),OLD.health_epoch+1,OLD.health_epoch);
CREATE TABLE IF NOT EXISTS health_clock (id integer PRIMARY KEY CHECK(id=1), revision bigint NOT NULL) ENGINE=InnoDB;
INSERT INTO health_clock(id,revision) VALUES(1,0) ON DUPLICATE KEY UPDATE id=id;
CREATE TABLE IF NOT EXISTS health_sample (
 sample_id char(64) PRIMARY KEY, old_id integer NOT NULL, device_id integer,
 epoch bigint NOT NULL, version bigint NOT NULL CHECK(version>0), revision bigint NOT NULL UNIQUE,
 payload json NOT NULL, source varchar(256) NOT NULL, source_device varchar(4096) NOT NULL,
 metric varchar(64) NOT NULL, semantics varchar(32) NOT NULL,
 sample_start bigint NOT NULL, sample_end bigint NOT NULL, deleted boolean NOT NULL,
 INDEX idx_health_owner(old_id,revision), INDEX idx_health_time(old_id,sample_start)
) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS health_change (
 revision bigint PRIMARY KEY, old_id integer NOT NULL, sample_id char(64) NOT NULL, payload json NOT NULL,
 INDEX idx_health_change_owner(old_id,revision)
) ENGINE=InnoDB;

-- All authoritative state stays in MySQL. No commercial server schema is implied.
CREATE TABLE IF NOT EXISTS device_command_profile (
 device_id bigint PRIMARY KEY, wire_type varchar(64) NOT NULL,
 protocol varchar(32) NOT NULL CHECK(protocol='yihe-pull-v1'), enabled boolean NOT NULL,
 updated_by bigint NOT NULL, updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS device_command (
 id bigint AUTO_INCREMENT PRIMARY KEY, device_id bigint NOT NULL, old_id bigint NOT NULL,
 epoch bigint NOT NULL, request_key varchar(64) NOT NULL, action varchar(32) NOT NULL,
 ttl_seconds integer NOT NULL CHECK(ttl_seconds BETWEEN 30 AND 3600),
 wire_type varchar(64) NOT NULL, channel varchar(16) NOT NULL, wire_action varchar(64) NOT NULL,
 status varchar(16) NOT NULL DEFAULT 'pending'
 CHECK(status IN ('pending','sent','acked','failed','timeout','cancelled')),
 attempts integer NOT NULL DEFAULT 0, max_attempts integer NOT NULL,
 delivery_token varchar(64), retry_at datetime(6) NOT NULL,
 expires_at datetime(6) NOT NULL, sent_at datetime(6), acked_at datetime(6),
 created_by bigint NOT NULL, created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE(device_id,epoch,request_key), INDEX idx_command_ready(status,retry_at),
 INDEX idx_command_device(device_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS metric_daily (
 cache_key char(64) PRIMARY KEY, old_id bigint NOT NULL,
 source varchar(256) NOT NULL, source_device varchar(4096) NOT NULL,
 metric varchar(64) NOT NULL, semantics varchar(32) NOT NULL,
 record_day date NOT NULL, offset_minutes integer NOT NULL,
 dirty boolean NOT NULL DEFAULT true, computed_revision bigint NOT NULL DEFAULT 0,
 result json, error_text varchar(512) NOT NULL DEFAULT '',
 updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), INDEX idx_daily_dirty(dirty,updated_at),
 INDEX idx_daily_owner(old_id,record_day)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS device_fence (
 id bigint AUTO_INCREMENT PRIMARY KEY, device_id bigint NOT NULL, old_id bigint NOT NULL, epoch bigint NOT NULL,
 name varchar(64) NOT NULL, enabled boolean NOT NULL, revision bigint NOT NULL DEFAULT 1,
 coordinates json NOT NULL, coordinate_system varchar(16) NOT NULL CHECK(coordinate_system='WGS84'),
 timezone varchar(64) NOT NULL, time_begin integer NOT NULL CHECK(time_begin>=0 AND time_begin<86400),
 time_end integer NOT NULL CHECK(time_end>0 AND time_end<=86400),
 min_points integer NOT NULL CHECK(min_points BETWEEN 2 AND 20),
 dwell_seconds integer NOT NULL CHECK(dwell_seconds BETWEEN 1 AND 3600),
 max_accuracy double NOT NULL CHECK(max_accuracy>0 AND max_accuracy<=1000),
 updated_by bigint NOT NULL, updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX idx_fence_device(device_id,epoch,enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS fence_state (
 fence_id bigint PRIMARY KEY, revision bigint NOT NULL, payload json NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS fence_event (
 id bigint AUTO_INCREMENT PRIMARY KEY, fence_id bigint NOT NULL, fence_revision bigint NOT NULL,
 old_id bigint NOT NULL, device_id bigint NOT NULL, epoch bigint NOT NULL,
 direction varchar(8) NOT NULL CHECK(direction IN ('entry','exit')),
 lng double NOT NULL, lat double NOT NULL, accuracy double NOT NULL,
 occurred_at datetime(6) NOT NULL, created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 incident_id bigint, UNIQUE(fence_id,fence_revision,occurred_at), INDEX idx_fence_event_owner(old_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS alert_review (
 id bigint AUTO_INCREMENT PRIMARY KEY, entity varchar(16) NOT NULL CHECK(entity IN ('care','ratepre')),
 record_id bigint NOT NULL, kind varchar(32) NOT NULL,
 verdict varchar(16) NOT NULL CHECK(verdict IN ('confirmed','false_alarm')),
 reason varchar(2000) NOT NULL, actor_id bigint NOT NULL,
 created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX idx_review_target(entity,record_id,kind,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

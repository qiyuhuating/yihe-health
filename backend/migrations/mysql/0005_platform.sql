ALTER TABLE alert_review MODIFY reason text NOT NULL;
ALTER TABLE care_notification_outbox ADD COLUMN exhausted boolean NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS web_session (
 id_hash char(64) PRIMARY KEY, role varchar(16) NOT NULL, actor_id bigint NOT NULL,
 payload mediumtext NOT NULL, expires_at datetime(6) NOT NULL,
 INDEX idx_session_actor(role,actor_id), INDEX idx_session_expiry(expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS auth_factor (
	role varchar(16) NOT NULL, actor_id bigint NOT NULL, secret text NOT NULL,
	generation char(36) NOT NULL,
 enabled boolean NOT NULL DEFAULT false, pending_until datetime(6) NOT NULL,
 last_counter bigint NOT NULL DEFAULT -1, recovery_hashes json NOT NULL,
 PRIMARY KEY(role,actor_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS login_audit (
 id bigint AUTO_INCREMENT PRIMARY KEY, account_hash char(64) NOT NULL, ip_hash char(64) NOT NULL,
 role varchar(16), actor_id bigint, outcome varchar(32) NOT NULL,
 request_id varchar(64) NOT NULL, created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX idx_login_attempt(account_hash,ip_hash,created_at), INDEX idx_login_history(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS message_outbox (
 id bigint AUTO_INCREMENT PRIMARY KEY, topic varchar(32) NOT NULL, source_key varchar(96) NOT NULL,
 family_id bigint NOT NULL, channel varchar(16) NOT NULL, recipient text NOT NULL,
 status varchar(16) NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','delivered','failed','exhausted')),
 attempts integer NOT NULL DEFAULT 0, retry_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 lease_token varchar(64), last_attempt_at datetime(6), last_error varchar(256),
 created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), delivered_at datetime(6),
 UNIQUE(topic,source_key,family_id,channel), INDEX idx_message_claim(status,retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS rule_policy (
 device_id bigint PRIMARY KEY, revision bigint NOT NULL, payload json NOT NULL,
 updated_by bigint NOT NULL, updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS rule_state (
 device_id bigint NOT NULL, epoch bigint NOT NULL, kind varchar(32) NOT NULL,
 observation_key varchar(96) NOT NULL, streak integer NOT NULL, active boolean NOT NULL,
 incident_id bigint, evidence json NOT NULL, updated_at datetime(6) NOT NULL,
 PRIMARY KEY(device_id,epoch,kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

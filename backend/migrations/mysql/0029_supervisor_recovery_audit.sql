CREATE TABLE IF NOT EXISTS security_audit_log (
 id char(36) PRIMARY KEY,
 action varchar(64) NOT NULL,
 os_user varchar(255) NOT NULL,
 os_uid varchar(191) NOT NULL,
 host varchar(255) NOT NULL,
 reason text NOT NULL,
 admin_id bigint NOT NULL,
 outcome varchar(16) NOT NULL CHECK(outcome IN ('granted','denied')),
 decision varchar(64) NOT NULL,
 before_state json NOT NULL,
 after_state json NOT NULL,
 occurred_at datetime(6) NOT NULL,
 CHECK(outcome<>'granted' OR CHAR_LENGTH(TRIM(reason))>0),
 INDEX idx_security_audit_time(occurred_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TRIGGER security_audit_no_update BEFORE UPDATE ON security_audit_log
 FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='security audit is append-only';
CREATE TRIGGER security_audit_no_delete BEFORE DELETE ON security_audit_log
 FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='security audit is append-only';

CREATE TABLE IF NOT EXISTS security_alert_outbox (
 audit_id char(36) PRIMARY KEY,
 status varchar(16) NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','delivered')),
 attempts bigint NOT NULL DEFAULT 0,
 lease_token char(36),
 available_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 delivered_at datetime(6),
 last_error varchar(64) NOT NULL DEFAULT '',
 CONSTRAINT fk_security_alert_audit FOREIGN KEY(audit_id) REFERENCES security_audit_log(id),
 INDEX idx_security_alert_ready(status,available_at,audit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

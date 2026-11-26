-- MySQL 8.0.16+ / InnoDB, dedicated database only.
CREATE TABLE IF NOT EXISTS alarm_precess (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  status varchar(20) DEFAULT NULL,
  old_id bigint DEFAULT NULL,
  device_id bigint DEFAULT NULL,
  be_time datetime(6) DEFAULT NULL,
  lng varchar(20) DEFAULT NULL,
  lat varchar(20) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS customer (
  customer_id bigint AUTO_INCREMENT PRIMARY KEY,
  customer_name varchar(20) DEFAULT NULL,
  customer_age bigint DEFAULT NULL,
  customer_sex char(1) DEFAULT NULL,
  customer_phone varchar(12) DEFAULT NULL,
  customer_address varchar(20) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS family (
  family_tou_pic varchar(50) DEFAULT 'kai.jpg',
  family_id bigint AUTO_INCREMENT PRIMARY KEY,
  family_name varchar(10) DEFAULT NULL,
  family_relation varchar(10) DEFAULT NULL,
  family_phone varchar(12) DEFAULT NULL,
  family_sex char(1) DEFAULT NULL,
  family_address varchar(20) DEFAULT NULL,
  family_password varchar(60) DEFAULT NULL,
  CONSTRAINT uk_family_phone UNIQUE (family_phone)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS invite_code (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  code varchar(20) NOT NULL,
  old_id bigint DEFAULT NULL,
  create_user varchar(20) DEFAULT '管理员',
  create_time datetime(6) DEFAULT NULL,
  expire_time datetime(6) DEFAULT NULL,
  used smallint NOT NULL DEFAULT 0,
  used_time datetime(6) DEFAULT NULL,
  used_family_id bigint DEFAULT NULL,
  CONSTRAINT uk_invite_code_code UNIQUE (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_invite_code_old ON invite_code(old_id);

CREATE TABLE IF NOT EXISTS nice_count (
  phone varchar(30) DEFAULT NULL,
  said_id bigint DEFAULT NULL,
  add_id bigint AUTO_INCREMENT PRIMARY KEY
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_nice_count_said_phone ON nice_count(said_id, phone);

CREATE TABLE IF NOT EXISTS oldperson (
  old_id bigint AUTO_INCREMENT PRIMARY KEY,
  name varchar(10) DEFAULT NULL,
  age bigint DEFAULT NULL,
  sex char(1) DEFAULT NULL,
  phone varchar(12) DEFAULT NULL,
  room varchar(5) DEFAULT NULL,
  illness varchar(20) DEFAULT NULL,
  warn varchar(5) DEFAULT NULL,
  food_like varchar(20) DEFAULT NULL,
  food_hate varchar(20) DEFAULT NULL,
  drag varchar(20) DEFAULT NULL,
  family_id bigint DEFAULT NULL,
  customer_id bigint DEFAULT NULL,
  be_time date DEFAULT NULL,
  bothdate date DEFAULT NULL,
  device_id bigint DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS pic (
  pic_id bigint NOT NULL,
  pic_name varchar(50) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS pic_show (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  pic_name varchar(50) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS ratepre (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  device_id bigint DEFAULT NULL,
  old_id bigint DEFAULT NULL,
  rate_state varchar(5) DEFAULT NULL,
  pre_state varchar(5) DEFAULT NULL,
  create_time datetime(6) DEFAULT NULL,
  hr bigint DEFAULT NULL,
  bp bigint DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS said (
  said_id bigint AUTO_INCREMENT PRIMARY KEY,
  said varchar(2000) DEFAULT NULL,
  text_id bigint DEFAULT NULL,
  back varchar(2000) DEFAULT NULL,
  said_time datetime(6) DEFAULT NULL,
  back_time datetime(6) DEFAULT NULL,
  person_name varchar(10) DEFAULT NULL,
  said_phone varchar(30) DEFAULT NULL,
  tou_pic varchar(50) DEFAULT NULL,
  nice_count bigint DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS `text` (
  text_id bigint AUTO_INCREMENT PRIMARY KEY,
  title varchar(200) DEFAULT NULL,
  text_main varchar(10000) DEFAULT NULL,
  time datetime(6) DEFAULT NULL,
  pic_id bigint DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS users (
  user_id bigint AUTO_INCREMENT PRIMARY KEY,
  user_name varchar(10) DEFAULT NULL,
  password varchar(60) DEFAULT NULL,
  role varchar(10) DEFAULT NULL,
  user_phone varchar(20) DEFAULT NULL,
  CONSTRAINT uk_users_phone UNIQUE (user_phone)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS watch_day_info (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  device_id bigint DEFAULT NULL,
  sleep bigint DEFAULT NULL,
  step bigint DEFAULT NULL,
  sleep_sate char(1) DEFAULT NULL,
  create_time date DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS watch_info (
  device_id bigint AUTO_INCREMENT PRIMARY KEY,
  op_time date DEFAULT NULL,
  bpf bigint DEFAULT NULL,
  bpl bigint DEFAULT NULL,
  hrf bigint DEFAULT NULL,
  hrl bigint DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS device (
  device_id bigint AUTO_INCREMENT PRIMARY KEY,
  device_no varchar(32) NOT NULL,
  secret varchar(64) NOT NULL,
  alias varchar(32) DEFAULT NULL,
  model varchar(32) DEFAULT NULL,
  app_version varchar(20) DEFAULT NULL,
  battery bigint DEFAULT NULL,
  old_id bigint DEFAULT NULL,
  family_id bigint DEFAULT NULL,
  status smallint NOT NULL DEFAULT 1,
  bind_time datetime(6) DEFAULT NULL,
  last_time datetime(6) DEFAULT NULL,
  last_lng varchar(20) DEFAULT NULL,
  last_lat varchar(20) DEFAULT NULL,
  create_time datetime(6) DEFAULT NULL,
  CONSTRAINT uk_device_no UNIQUE (device_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_device_old ON device(old_id);

CREATE TABLE IF NOT EXISTS device_location (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  device_id bigint DEFAULT NULL,
  old_id bigint DEFAULT NULL,
  lng varchar(20) DEFAULT NULL,
  lat varchar(20) DEFAULT NULL,
  source varchar(20) DEFAULT NULL,
  be_time datetime(6) DEFAULT NULL,
  create_time datetime(6) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_loc_old_time ON device_location(old_id, id);
CREATE INDEX idx_loc_device_time ON device_location(device_id, id);

CREATE TABLE IF NOT EXISTS bind_code (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  code varchar(20) NOT NULL,
  old_id bigint DEFAULT NULL,
  family_id bigint DEFAULT NULL,
  create_user varchar(20) DEFAULT NULL,
  create_time datetime(6) DEFAULT NULL,
  expire_time datetime(6) DEFAULT NULL,
  used smallint NOT NULL DEFAULT 0,
  used_time datetime(6) DEFAULT NULL,
  used_device_id bigint DEFAULT NULL,
  CONSTRAINT uk_bind_code UNIQUE (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_bind_code_old ON bind_code(old_id);

ALTER TABLE ratepre ADD COLUMN rate_handled bigint DEFAULT 0 NOT NULL;
ALTER TABLE ratepre ADD COLUMN pre_handled bigint DEFAULT 0 NOT NULL;
ALTER TABLE ratepre ADD COLUMN rate_handled_at datetime(6);
ALTER TABLE ratepre ADD COLUMN pre_handled_at datetime(6);
ALTER TABLE device ADD COLUMN person_handled bigint DEFAULT 0 NOT NULL;
ALTER TABLE device ADD COLUMN rate_handled bigint DEFAULT 0 NOT NULL;
ALTER TABLE device ADD COLUMN pre_handled bigint DEFAULT 0 NOT NULL;
ALTER TABLE device ADD COLUMN person_handled_at datetime(6);
ALTER TABLE device ADD COLUMN boot_seq bigint DEFAULT 0 NOT NULL;
ALTER TABLE device ADD COLUMN handled_seq bigint DEFAULT 0 NOT NULL;
ALTER TABLE device ADD COLUMN last_online_at datetime(6);

CREATE INDEX idx_ratepre_old_time ON ratepre(old_id,create_time,id);
CREATE INDEX idx_ratepre_device_time ON ratepre(device_id,create_time,id);
CREATE INDEX idx_alarm_old_time ON alarm_precess(old_id,be_time,id);
CREATE INDEX idx_old_family ON oldperson(family_id);
CREATE INDEX idx_said_text ON said(text_id,said_time);
CREATE INDEX idx_pic_group ON pic(pic_id);

CREATE TABLE IF NOT EXISTS picture_group (id bigint AUTO_INCREMENT PRIMARY KEY);

CREATE TABLE IF NOT EXISTS app_metadata (id bigint PRIMARY KEY, schema_version bigint NOT NULL);
INSERT INTO app_metadata(id,schema_version) SELECT 1,1 WHERE NOT EXISTS (SELECT 1 FROM app_metadata WHERE id=1);




CREATE TABLE IF NOT EXISTS watch_day_info_archive LIKE watch_day_info;

CREATE UNIQUE INDEX uk_day_device_date ON watch_day_info(device_id,create_time);






CREATE TABLE IF NOT EXISTS alarm_process (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  status varchar(20) DEFAULT NULL,
  old_id bigint DEFAULT NULL,
  device_id bigint DEFAULT NULL,
  be_time datetime(6) DEFAULT NULL,
  lng varchar(20) DEFAULT NULL,
  lat varchar(20) DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;



ALTER TABLE device ADD CONSTRAINT fk_device_oldperson
  FOREIGN KEY (old_id) REFERENCES oldperson(old_id) ON DELETE RESTRICT;




CREATE TABLE IF NOT EXISTS care_incident (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  source_alarm_id bigint,
  FOREIGN KEY (source_alarm_id) REFERENCES alarm_precess(id) ON DELETE SET NULL,
  request_id varchar(64),
  old_id bigint NOT NULL,
  device_id bigint NOT NULL,
  kind varchar(32) NOT NULL DEFAULT 'sos',
  state varchar(32) NOT NULL DEFAULT 'received'
    CHECK (state IN ('received','notifying','notified','acknowledged','in_progress','resolved','verified','escalated','review_overdue')),
  severity varchar(16) NOT NULL DEFAULT 'critical',
  created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  acknowledged_by varchar(128),
  acknowledged_at datetime(6),
  resolved_by varchar(128),
  resolved_at datetime(6),
  resolution_note text,
  verified_by varchar(128),
  verified_at datetime(6),
  due_at datetime(6) NOT NULL DEFAULT (now() + interval 5 minute)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE UNIQUE INDEX idx_care_incident_device_request ON care_incident(device_id,request_id);
CREATE INDEX idx_care_incident_open_due
  ON care_incident (due_at, created_at);

CREATE TABLE IF NOT EXISTS care_incident_event (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  incident_id bigint NOT NULL,
  FOREIGN KEY (incident_id) REFERENCES care_incident(id) ON DELETE RESTRICT,
  actor varchar(128) NOT NULL,
  action varchar(40) NOT NULL,
  note text,
  created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_care_incident_event_incident
  ON care_incident_event (incident_id, created_at);

CREATE TABLE IF NOT EXISTS care_notification_outbox (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  incident_id bigint NOT NULL,
  FOREIGN KEY (incident_id) REFERENCES care_incident(id) ON DELETE RESTRICT,
  channel varchar(20) NOT NULL,
  recipient varchar(128) NOT NULL,
  status varchar(20) NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','sending','sent','delivered','failed')),
  attempts bigint NOT NULL DEFAULT 0,
  last_attempt_at datetime(6),
  retry_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  sent_at datetime(6),
  delivered_at datetime(6),
  last_error text,
  created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  UNIQUE (incident_id, channel, recipient)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_care_notification_retry ON care_notification_outbox (retry_at, id);



CREATE TABLE IF NOT EXISTS care_staff_permission (
  admin_id bigint PRIMARY KEY,
  FOREIGN KEY (admin_id) REFERENCES users(user_id) ON DELETE CASCADE,
  duty varchar(24) NOT NULL CHECK (duty IN ('supervisor','caregiver','auditor')),
  assigned_by bigint,
  assigned_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS care_access_audit (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  admin_id bigint NOT NULL,
  action varchar(64) NOT NULL,
  incident_id bigint,
  subject_admin_id bigint,
  created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_care_access_audit_actor_time ON care_access_audit(admin_id,created_at DESC);
CREATE INDEX idx_care_access_audit_created ON care_access_audit(created_at);


CREATE TABLE IF NOT EXISTS care_sensitive_access_audit (
  id bigint AUTO_INCREMENT PRIMARY KEY,
  actor_id bigint NOT NULL,
  actor_role varchar(16) NOT NULL,
  resource varchar(128) NOT NULL,
  method varchar(8) NOT NULL,
  created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE INDEX idx_care_sensitive_access_time
  ON care_sensitive_access_audit(created_at DESC);
CREATE INDEX idx_care_sensitive_access_actor
  ON care_sensitive_access_audit(actor_id,created_at DESC);


CREATE TABLE IF NOT EXISTS care_worker_heartbeat (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton=1),
  last_seen_at datetime(6) NOT NULL,
  started_at datetime(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

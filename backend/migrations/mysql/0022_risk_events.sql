CREATE TABLE IF NOT EXISTS risk_ip_rules (
  rule_id VARCHAR(48) NOT NULL PRIMARY KEY,
  cidr VARCHAR(80) NOT NULL,
  action VARCHAR(20) NOT NULL,
  reason VARCHAR(240) NOT NULL,
  actor VARCHAR(128) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  expires_at DATETIME(6) NULL,
  INDEX idx_risk_rule_expires (expires_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS risk_login_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  event_key CHAR(32) NOT NULL UNIQUE,
  occurred_at DATETIME(6) NOT NULL,
  ip_address VARCHAR(45) NOT NULL,
  account_mask VARCHAR(32) NOT NULL,
  event_type VARCHAR(48) NOT NULL,
  detail VARCHAR(255) NOT NULL,
  device_json JSON NOT NULL,
  network_kind VARCHAR(48) NOT NULL,
  network_confidence TINYINT UNSIGNED NOT NULL,
  network_provider VARCHAR(128) NOT NULL,
  network_source VARCHAR(128) NOT NULL,
  INDEX idx_risk_time (occurred_at, id),
  INDEX idx_risk_ip_time (ip_address, occurred_at)
) ENGINE=InnoDB;

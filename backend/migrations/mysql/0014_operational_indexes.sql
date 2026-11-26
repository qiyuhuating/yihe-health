CREATE INDEX idx_care_outbox_age ON care_notification_outbox(status,created_at);
CREATE INDEX idx_login_account_time ON login_audit(account_hash,created_at,outcome);
CREATE INDEX idx_login_pair_time ON login_audit(account_hash,ip_hash,created_at,outcome);

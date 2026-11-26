DROP INDEX idx_care_outbox_age ON care_notification_outbox;
DROP INDEX idx_login_account_time ON login_audit;
DROP INDEX idx_login_pair_time ON login_audit;
DELETE FROM schema_migrations WHERE name='mysql/0014_operational_indexes.sql';

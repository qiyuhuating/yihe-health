-- Stop every app instance and worker. Export sessions, factors, outbox, audit,
-- policies and rule evidence first. Existing encrypted care notes require the
-- original key and an explicit offline decryption before using an old binary.
-- MySQL DDL commits implicitly. Delivered notifications cannot be undone.
DROP TABLE IF EXISTS rule_state;
DROP TABLE IF EXISTS rule_policy;
DROP TABLE IF EXISTS message_outbox;
DROP TABLE IF EXISTS login_audit;
DROP TABLE IF EXISTS auth_factor;
DROP TABLE IF EXISTS web_session;
UPDATE care_notification_outbox SET retry_at='9999-12-31 00:00:00' WHERE exhausted=true;
ALTER TABLE care_notification_outbox DROP COLUMN exhausted;
DELETE FROM schema_migrations WHERE name='mysql/0005_platform.sql';

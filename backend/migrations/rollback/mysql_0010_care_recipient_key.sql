-- 回滚收件人加密：恢复明文列上的唯一约束。仅当该迁移尚未在生产产生密文行时安全。
-- 新唯一索引是 incident_id 外键的支撑索引，必须先摘除 FK 再重建旧索引。
ALTER TABLE care_notification_outbox DROP FOREIGN KEY care_notification_outbox_ibfk_1;
ALTER TABLE care_notification_outbox DROP INDEX uk_care_notification_recipient;
ALTER TABLE care_notification_outbox ADD UNIQUE KEY incident_id (incident_id, channel, recipient);
ALTER TABLE care_notification_outbox DROP COLUMN recipient_key;
ALTER TABLE care_notification_outbox ADD CONSTRAINT FOREIGN KEY (incident_id) REFERENCES care_incident(id) ON DELETE RESTRICT;
DELETE FROM schema_migrations WHERE name='mysql/0010_care_recipient_key.sql';

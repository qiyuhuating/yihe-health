-- 照护通知收件人手机号改为密文存储（securestore AEAD，与 message_outbox 一致）。
-- 密文每次密封都是随机的，不能再作唯一约束；新增 recipient_key 存收件人的
-- 确定性摘要（配置密钥时为 HMAC-SHA256，无密钥回退 SHA-256）承接去重，
-- 保证 escalate 周期重试与语音升级不会因密文不同而插入重复通知。
-- 历史明文行按无密钥回退口径回填，worker 对其原样投递、无需迁移即可消化。
ALTER TABLE care_notification_outbox ADD COLUMN recipient_key char(64) NOT NULL DEFAULT '' AFTER recipient;
UPDATE care_notification_outbox SET recipient_key=SHA2(CONCAT('care-notification-recipient',0x00,recipient),256);
ALTER TABLE care_notification_outbox DROP INDEX incident_id, ADD UNIQUE KEY uk_care_notification_recipient (incident_id, channel, recipient_key);

-- One durable coalescing task per device. Device writes and their rule wakeup
-- share the same transaction, including batch health, heartbeat, SOS and bind.
CREATE TABLE rule_task (
 device_id bigint PRIMARY KEY,
 epoch bigint NOT NULL,
 eligible boolean NOT NULL,
 due_at datetime(6) NULL,
 INDEX idx_rule_task_due(due_at,device_id),
 CONSTRAINT fk_rule_task_device FOREIGN KEY(device_id) REFERENCES device(device_id) ON DELETE CASCADE
) ENGINE=InnoDB;

INSERT INTO rule_task(device_id,epoch,eligible,due_at)
 SELECT device_id,health_epoch,old_id IS NOT NULL AND status<>0,
 IF(old_id IS NOT NULL AND status<>0,CURRENT_TIMESTAMP(6),NULL) FROM device;

CREATE TRIGGER rule_task_device_insert AFTER INSERT ON device FOR EACH ROW
 INSERT INTO rule_task(device_id,epoch,eligible,due_at)
 VALUES(NEW.device_id,NEW.health_epoch,NEW.old_id IS NOT NULL AND NEW.status<>0,
 IF(NEW.old_id IS NOT NULL AND NEW.status<>0,CURRENT_TIMESTAMP(6),NULL));

CREATE TRIGGER rule_task_device_update AFTER UPDATE ON device FOR EACH ROW
 UPDATE rule_task SET
 due_at=IF(NEW.old_id IS NOT NULL AND NEW.status<>0,LEAST(COALESCE(due_at,CURRENT_TIMESTAMP(6)),CURRENT_TIMESTAMP(6)),NULL),
 epoch=NEW.health_epoch,eligible=NEW.old_id IS NOT NULL AND NEW.status<>0
 WHERE device_id=NEW.device_id;

-- Policy/threshold writes can wake a dormant enabled-binding task without
-- taking a second device lock. The evaluator reads these inputs only after
-- locking the task, and uses nonlocking reads of the policy/threshold rows.
CREATE TRIGGER rule_task_policy_insert AFTER INSERT ON rule_policy FOR EACH ROW
 UPDATE rule_task SET due_at=LEAST(COALESCE(due_at,CURRENT_TIMESTAMP(6)),CURRENT_TIMESTAMP(6)) WHERE device_id=NEW.device_id AND eligible;
CREATE TRIGGER rule_task_policy_update AFTER UPDATE ON rule_policy FOR EACH ROW
 UPDATE rule_task SET due_at=LEAST(COALESCE(due_at,CURRENT_TIMESTAMP(6)),CURRENT_TIMESTAMP(6)) WHERE device_id=NEW.device_id AND eligible;
CREATE TRIGGER rule_task_policy_delete AFTER DELETE ON rule_policy FOR EACH ROW
 UPDATE rule_task SET due_at=LEAST(COALESCE(due_at,CURRENT_TIMESTAMP(6)),CURRENT_TIMESTAMP(6)) WHERE device_id=OLD.device_id AND eligible;

CREATE TRIGGER rule_task_watch_insert AFTER INSERT ON watch_info FOR EACH ROW
 UPDATE rule_task SET due_at=LEAST(COALESCE(due_at,CURRENT_TIMESTAMP(6)),CURRENT_TIMESTAMP(6)) WHERE device_id=NEW.device_id AND eligible;
CREATE TRIGGER rule_task_watch_update AFTER UPDATE ON watch_info FOR EACH ROW
 UPDATE rule_task SET due_at=LEAST(COALESCE(due_at,CURRENT_TIMESTAMP(6)),CURRENT_TIMESTAMP(6)) WHERE device_id=NEW.device_id AND eligible;
CREATE TRIGGER rule_task_watch_delete AFTER DELETE ON watch_info FOR EACH ROW
 UPDATE rule_task SET due_at=LEAST(COALESCE(due_at,CURRENT_TIMESTAMP(6)),CURRENT_TIMESTAMP(6)) WHERE device_id=OLD.device_id AND eligible;

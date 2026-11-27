-- Stop all service instances before restoring a matching older application.
DROP TRIGGER IF EXISTS rule_task_watch_delete;
DROP TRIGGER IF EXISTS rule_task_watch_update;
DROP TRIGGER IF EXISTS rule_task_watch_insert;
DROP TRIGGER IF EXISTS rule_task_policy_delete;
DROP TRIGGER IF EXISTS rule_task_policy_update;
DROP TRIGGER IF EXISTS rule_task_policy_insert;
DROP TRIGGER IF EXISTS rule_task_device_update;
DROP TRIGGER IF EXISTS rule_task_device_insert;
DROP TABLE IF EXISTS rule_task;
DELETE FROM schema_migrations WHERE name='mysql/0044_rule_tasks.sql';

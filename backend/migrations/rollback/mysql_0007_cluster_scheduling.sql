-- Stop ALL writers before downgrade. Never mix legacy global-clock writers
-- with owner-range writers. Keep health_clock at its reserved high-water mark:
-- it is above every committed revision, including partially unused ranges.
DROP INDEX idx_message_channel_ready ON message_outbox;
DROP INDEX idx_message_lease ON message_outbox;
DROP TABLE IF EXISTS health_owner_clock;
DELETE FROM schema_migrations WHERE name='mysql/0007_cluster_scheduling.sql';

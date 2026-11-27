-- Drain all writers before rollback. Discard unused future ranges rather
-- than allowing the old allocator to issue revisions inside another range.
UPDATE health_owner_clock SET range_end=revision,range_start=0 WHERE range_start>revision;
ALTER TABLE health_owner_clock DROP CHECK ck_health_owner_range_start, DROP COLUMN range_start;
DELETE FROM schema_migrations WHERE name='mysql/0050_health_owner_range_start.sql';

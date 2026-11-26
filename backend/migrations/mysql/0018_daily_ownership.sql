-- Freeze the currently visible legacy association. Earlier ownership cannot be reconstructed.
ALTER TABLE watch_day_info ADD COLUMN old_id bigint DEFAULT NULL;
ALTER TABLE watch_day_info ADD COLUMN binding_epoch bigint NOT NULL DEFAULT 0;
ALTER TABLE watch_day_info_archive ADD COLUMN step_observed_at datetime(6) DEFAULT NULL;
ALTER TABLE watch_day_info_archive ADD COLUMN old_id bigint DEFAULT NULL;
ALTER TABLE watch_day_info_archive ADD COLUMN binding_epoch bigint NOT NULL DEFAULT 0;
UPDATE watch_day_info w INNER JOIN device d ON d.device_id=w.device_id
SET w.old_id=d.old_id,w.binding_epoch=d.health_epoch
WHERE w.old_id IS NULL AND w.binding_epoch=0 AND d.old_id IS NOT NULL;
-- Mark unassigned legacy rows so replay cannot assign them after a later binding.
UPDATE watch_day_info SET binding_epoch=-1 WHERE old_id IS NULL AND binding_epoch=0;
CREATE INDEX idx_day_owner ON watch_day_info(old_id,device_id,create_time);
CREATE INDEX idx_day_archive_owner ON watch_day_info_archive(old_id,device_id,create_time);

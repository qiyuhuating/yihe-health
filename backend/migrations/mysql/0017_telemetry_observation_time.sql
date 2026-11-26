-- Legacy rows have no trustworthy observation time. The first timestamped
-- report establishes ordering without inventing timestamps for old values.
ALTER TABLE watch_day_info ADD COLUMN step_observed_at datetime(6) DEFAULT NULL;
CREATE INDEX idx_loc_old_observed ON device_location(old_id,be_time,id);
CREATE INDEX idx_loc_device_observed ON device_location(device_id,be_time,id);

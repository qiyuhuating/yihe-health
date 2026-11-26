-- Duration and classification can arrive independently. Unknown legacy times remain NULL.
ALTER TABLE watch_day_info ADD COLUMN sleep_observed_at datetime(6) DEFAULT NULL;
ALTER TABLE watch_day_info ADD COLUMN sleep_state_observed_at datetime(6) DEFAULT NULL;
ALTER TABLE watch_day_info_archive ADD COLUMN sleep_observed_at datetime(6) DEFAULT NULL;
ALTER TABLE watch_day_info_archive ADD COLUMN sleep_state_observed_at datetime(6) DEFAULT NULL;

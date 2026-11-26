ALTER TABLE health_change
 MODIFY payload JSON NULL,
 ADD COLUMN payload_cipher MEDIUMTEXT DEFAULT NULL,
 ADD CONSTRAINT ck_health_change_payload CHECK ((payload IS NULL) <> (payload_cipher IS NULL));

ALTER TABLE health_sample_latest
 MODIFY payload JSON NULL,
 ADD COLUMN payload_cipher MEDIUMTEXT DEFAULT NULL,
 ADD CONSTRAINT ck_health_latest_payload CHECK ((payload IS NULL) <> (payload_cipher IS NULL));

ALTER TABLE metric_daily
 ADD COLUMN result_cipher MEDIUMTEXT DEFAULT NULL,
 ADD CONSTRAINT ck_metric_daily_result CHECK (result IS NULL OR result_cipher IS NULL);

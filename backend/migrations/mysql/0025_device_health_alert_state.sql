-- Materialized current health-alert state. Historical tables remain the audit source.
CREATE TABLE device_health_alert_state (
 device_id bigint PRIMARY KEY,
 binding_epoch bigint NOT NULL,
 heart_rate_alert boolean NOT NULL DEFAULT false,
 blood_pressure_alert boolean NOT NULL DEFAULT false,
 sleep_alert boolean NOT NULL DEFAULT false,
 vitals_observed_at datetime(6) DEFAULT NULL,
 sleep_observed_at datetime(6) DEFAULT NULL,
 updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 CONSTRAINT fk_health_alert_state_device FOREIGN KEY(device_id) REFERENCES device(device_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO device_health_alert_state(
 device_id,binding_epoch,heart_rate_alert,blood_pressure_alert,sleep_alert,vitals_observed_at,sleep_observed_at
)
SELECT d.device_id,d.health_epoch,
 EXISTS(SELECT 1 FROM ratepre r WHERE r.device_id=d.device_id AND r.old_id=d.old_id
   AND r.rate_state='1' AND (d.bind_time IS NULL OR r.create_time>=d.bind_time)),
 EXISTS(SELECT 1 FROM ratepre r WHERE r.device_id=d.device_id AND r.old_id=d.old_id
   AND r.pre_state='1' AND (d.bind_time IS NULL OR r.create_time>=d.bind_time)),
 COALESCE((SELECT w.sleep_sate='1' FROM watch_day_info w
   WHERE w.device_id=d.device_id AND w.old_id=d.old_id AND w.binding_epoch=d.health_epoch AND w.sleep_sate IS NOT NULL
   ORDER BY w.sleep_state_observed_at DESC,w.create_time DESC,w.id DESC LIMIT 1),false),
 (SELECT MAX(r.create_time) FROM ratepre r WHERE r.device_id=d.device_id AND r.old_id=d.old_id
   AND (d.bind_time IS NULL OR r.create_time>=d.bind_time)),
 (SELECT MAX(w.sleep_state_observed_at) FROM watch_day_info w
   WHERE w.device_id=d.device_id AND w.old_id=d.old_id AND w.binding_epoch=d.health_epoch)
FROM device d;

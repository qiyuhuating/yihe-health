CREATE TABLE resident_location_alert_state (
 old_id bigint PRIMARY KEY,
 alarm_id bigint NOT NULL,
 active boolean NOT NULL DEFAULT false,
 observed_at datetime(6) DEFAULT NULL,
 updated_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 CONSTRAINT fk_location_alert_state_resident FOREIGN KEY(old_id) REFERENCES oldperson(old_id) ON DELETE CASCADE,
 INDEX idx_location_alert_active(active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO resident_location_alert_state(old_id,alarm_id,active,observed_at)
SELECT old_id,id,COALESCE(status='1' AND CHAR_LENGTH(status)=1,false),be_time
FROM (
 SELECT old_id,id,status,be_time,
  ROW_NUMBER() OVER (PARTITION BY old_id ORDER BY be_time DESC,id DESC) AS rn
 FROM alarm_precess WHERE old_id IS NOT NULL
) latest
WHERE rn=1;

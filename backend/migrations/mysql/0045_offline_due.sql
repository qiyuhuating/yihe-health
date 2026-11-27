-- Persist the care online-session reset deadline as an indexed generated
-- column. Every update to the source fields maintains it atomically.
ALTER TABLE device
 ADD COLUMN offline_due_at datetime(6) GENERATED ALWAYS AS (
  CASE WHEN status=1 AND last_online_at IS NOT NULL AND last_time IS NOT NULL
   AND last_online_at>=last_time THEN DATE_ADD(last_time, INTERVAL 120 SECOND)
   ELSE NULL END
 ) VIRTUAL,
 ADD INDEX idx_device_offline_due(offline_due_at,device_id);

-- Historical context cannot be reconstructed from old coordinates or shared status.
ALTER TABLE alarm_precess ADD COLUMN kind varchar(32) NOT NULL DEFAULT 'unknown';
ALTER TABLE alarm_precess ADD COLUMN position_metadata text DEFAULT NULL;
ALTER TABLE device_location ADD COLUMN position_metadata text DEFAULT NULL;

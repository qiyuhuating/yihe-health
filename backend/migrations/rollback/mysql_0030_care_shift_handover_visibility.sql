DROP TABLE care_shift_handover_item;
DROP TABLE care_shift_handover;
DROP INDEX idx_care_incident_visibility_state_due ON care_incident;
ALTER TABLE care_incident DROP COLUMN visibility_group;
DELETE FROM schema_migrations WHERE name='mysql/0030_care_shift_handover_visibility.sql';

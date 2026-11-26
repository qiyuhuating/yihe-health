ALTER TABLE ratepre
 ADD COLUMN measurement_cipher TEXT DEFAULT NULL,
 ADD COLUMN measurement_pending BOOLEAN GENERATED ALWAYS AS (hr IS NOT NULL OR bp IS NOT NULL) VIRTUAL,
 ADD INDEX idx_ratepre_measurement_pending(measurement_pending,id),
 ADD CONSTRAINT ck_ratepre_measurement_storage CHECK (measurement_cipher IS NULL OR (hr IS NULL AND bp IS NULL));

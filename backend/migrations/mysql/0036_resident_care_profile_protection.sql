-- Expand only. Historical values require an explicit authenticated backfill.
ALTER TABLE oldperson
 ADD COLUMN care_profile_cipher text DEFAULT NULL,
 ADD COLUMN phone_lookup char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
 ADD COLUMN care_profile_protected boolean NOT NULL DEFAULT false,
 ADD INDEX idx_resident_phone_lookup(phone_lookup,old_id),
 ADD CONSTRAINT ck_resident_care_profile CHECK (
  (care_profile_protected=false AND care_profile_cipher IS NULL AND phone_lookup IS NULL) OR
  (care_profile_protected=true AND care_profile_cipher IS NOT NULL AND
   phone IS NULL AND room IS NULL AND illness IS NULL AND warn IS NULL AND
   food_like IS NULL AND food_hate IS NULL AND drag IS NULL AND
   (phone_lookup IS NULL OR phone_lookup REGEXP '^[0-9a-f]{64}$'))
 );
CREATE TABLE resident_profile_key_state (
 singleton tinyint NOT NULL PRIMARY KEY,
 lookup_verifier char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 CONSTRAINT ck_resident_profile_singleton CHECK (singleton=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

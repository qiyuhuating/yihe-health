-- Expand first. Historical values require explicit bounded backfill.
ALTER TABLE said
 ADD COLUMN said_phone_cipher text DEFAULT NULL,
 ADD COLUMN said_phone_protected boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT ck_said_phone_protection CHECK (
  (said_phone_protected=false AND said_phone_cipher IS NULL) OR
  (said_phone_protected=true AND said_phone IS NULL AND said_phone_cipher IS NOT NULL)
 );
ALTER TABLE nice_count
 ADD COLUMN phone_lookup char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
 ADD COLUMN phone_protected boolean NOT NULL DEFAULT false,
 ADD UNIQUE INDEX uk_nice_count_lookup(said_id,phone_lookup),
 ADD CONSTRAINT ck_like_phone_protection CHECK (
  (phone_protected=false AND phone_lookup IS NULL) OR
  (phone_protected=true AND said_id IS NOT NULL AND phone IS NULL AND phone_lookup IS NOT NULL AND phone_lookup REGEXP '^[0-9a-f]{64}$')
 );
CREATE TABLE social_phone_key_state (
 singleton tinyint NOT NULL PRIMARY KEY,
 lookup_verifier char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 CONSTRAINT ck_social_phone_singleton CHECK (singleton=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

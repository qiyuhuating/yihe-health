-- Expand only. Existing contacts require an explicit authenticated backfill.
ALTER TABLE customer
 ADD COLUMN contact_cipher text DEFAULT NULL,
 ADD COLUMN contact_protected boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT ck_guardian_contact CHECK (
  (contact_protected=false AND contact_cipher IS NULL) OR
  (contact_protected=true AND contact_cipher IS NOT NULL AND customer_phone IS NULL AND customer_address IS NULL)
 );
CREATE TABLE guardian_contact_key_state (
 singleton tinyint NOT NULL PRIMARY KEY,
 lookup_verifier char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 key_verifier text NOT NULL,
 CONSTRAINT ck_guardian_contact_singleton CHECK (singleton=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

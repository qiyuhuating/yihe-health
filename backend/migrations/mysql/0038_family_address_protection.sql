-- Preserve existing addresses until the explicit backfill is run.
ALTER TABLE family
 ADD COLUMN family_address_cipher text DEFAULT NULL,
 ADD COLUMN family_address_protected boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT ck_family_address_protection CHECK (
  (family_address_protected=false AND family_address_cipher IS NULL) OR
  (family_address_protected=true AND family_address IS NULL AND family_address_cipher IS NOT NULL)
 );

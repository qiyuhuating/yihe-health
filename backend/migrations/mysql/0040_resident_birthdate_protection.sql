-- Existing dates remain readable until the explicit backfill.
ALTER TABLE oldperson
 ADD COLUMN birthdate_cipher TEXT DEFAULT NULL,
 ADD COLUMN birthdate_protected BOOLEAN NOT NULL DEFAULT FALSE,
 ADD CONSTRAINT ck_resident_birthdate_protection CHECK (
  (birthdate_protected=FALSE AND birthdate_cipher IS NULL) OR
  (birthdate_protected=TRUE AND bothdate IS NULL AND birthdate_cipher IS NOT NULL)
 );

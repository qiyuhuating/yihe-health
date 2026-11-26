-- Expand first. Existing plaintext remains readable until explicit backfill.
ALTER TABLE family
 ADD COLUMN family_phone_cipher text DEFAULT NULL,
 ADD COLUMN family_phone_lookup char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
 ADD COLUMN family_phone_protected boolean NOT NULL DEFAULT false,
 ADD UNIQUE INDEX uk_family_phone_lookup(family_phone_lookup);

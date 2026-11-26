-- Additive expansion. Keep the original phone column readable until backfill.
-- MySQL applies each ALTER atomically. Duplicate-column errors on migration
-- replay are handled by the existing runner, without changing older checksums.
ALTER TABLE users
 ADD COLUMN user_phone_cipher text DEFAULT NULL,
 ADD COLUMN user_phone_lookup char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
 ADD COLUMN user_phone_protected boolean NOT NULL DEFAULT false,
 ADD UNIQUE INDEX uk_users_phone_lookup(user_phone_lookup);
CREATE TABLE IF NOT EXISTS account_phone_key_state (
 singleton tinyint PRIMARY KEY,
 lookup_verifier char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 CONSTRAINT ck_account_phone_singleton CHECK(singleton=1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

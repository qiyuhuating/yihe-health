-- The active administrator state is part of the persisted authorization boundary.
-- Retired accounts remain as audit identities and cannot be reactivated.
ALTER TABLE users ADD COLUMN account_status VARCHAR(16) NOT NULL DEFAULT 'active'
 CHECK (account_status IN ('active','suspended','retired'));

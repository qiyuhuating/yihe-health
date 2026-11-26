-- Offline downgrade: stop signed device ingress before reverting the migration ledger.
-- Keep replay proofs and their index so re-upgrading cannot reopen the nonce window.
-- Old server versions ignore this additive table.
-- Retain the ledger because the schema objects above remain installed.
SELECT name FROM schema_migrations WHERE name='mysql/0015_device_request_nonces.sql';

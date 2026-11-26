CREATE TABLE IF NOT EXISTS device_request_nonces (
    device_id BIGINT NOT NULL,
    nonce_digest BINARY(32) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    PRIMARY KEY (device_id, nonce_digest),
    INDEX idx_device_request_nonce_expiry (expires_at)
) ENGINE=InnoDB;

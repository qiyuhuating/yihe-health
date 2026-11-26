-- Additive InnoDB queue with no hardware secret or plaintext health payload stored.
CREATE TABLE IF NOT EXISTS telemetry_inbox (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    device_id BIGINT NOT NULL,
    elder_id BIGINT NOT NULL,
    binding_epoch BIGINT NOT NULL,
    request_id VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    payload LONGTEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INT NOT NULL DEFAULT 0,
    next_retry_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    completed_at DATETIME(6) NULL,
    last_error VARCHAR(500) NULL,
    UNIQUE KEY uq_telemetry_inbox_request (device_id,request_id),
    KEY idx_telemetry_inbox_pending (status,next_retry_at,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE IF NOT EXISTS telemetry_inbox_guard (
    id SMALLINT NOT NULL PRIMARY KEY
) ENGINE=InnoDB;
INSERT IGNORE INTO telemetry_inbox_guard(id) VALUES(1);

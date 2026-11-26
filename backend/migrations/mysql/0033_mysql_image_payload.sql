CREATE TABLE IF NOT EXISTS image_payload (
 name VARCHAR(255) PRIMARY KEY,
 payload MEDIUMBLOB NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX idx_image_payload_created_at(created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

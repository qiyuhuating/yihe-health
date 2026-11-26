-- Atomic acknowledgement for every numbered device report.
-- The primary key guards replay and the digest rejects ID reuse.
CREATE TABLE IF NOT EXISTS device_report_receipt (
  device_id bigint NOT NULL,
  binding_epoch bigint NOT NULL,
  request_id varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  payload_digest char(64) NOT NULL,
  result_json longtext NOT NULL,
  created_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY(device_id,binding_epoch,request_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

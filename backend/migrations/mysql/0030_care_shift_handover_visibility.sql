-- Fresh-install care queue visibility and shift handover.
-- The generated visibility bucket preserves the existing caregiver scope while
-- removing the mixed owner/state OR predicate from hot queue reads.
ALTER TABLE care_incident
  ADD COLUMN visibility_group VARCHAR(144) GENERATED ALWAYS AS (
    CASE
      WHEN state='escalated'
        OR (acknowledged_by IS NULL AND state IN ('received','notifying','notified'))
        THEN 'PUBLIC_POOL'
      WHEN acknowledged_by IS NOT NULL
        THEN CONCAT('ASSIGNED:', acknowledged_by)
      ELSE NULL
    END
  ) STORED;

CREATE INDEX idx_care_incident_visibility_state_due
  ON care_incident(visibility_group,state,due_at,id);

CREATE TABLE care_shift_handover (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  from_admin_id BIGINT NOT NULL,
  to_admin_id BIGINT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'pending'
    CHECK(status IN ('pending','accepted','cancelled')),
  note VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  accepted_at DATETIME(6),
  cancelled_at DATETIME(6),
  transferred_count BIGINT NOT NULL DEFAULT 0,
  pending_from_admin_id BIGINT GENERATED ALWAYS AS (
    CASE WHEN status='pending' THEN from_admin_id ELSE NULL END
  ) STORED,
  CONSTRAINT fk_care_handover_from FOREIGN KEY(from_admin_id) REFERENCES users(user_id) ON DELETE RESTRICT,
  CONSTRAINT fk_care_handover_to FOREIGN KEY(to_admin_id) REFERENCES users(user_id) ON DELETE RESTRICT,
  UNIQUE KEY uk_care_handover_pending_from(pending_from_admin_id),
  INDEX idx_care_handover_to_status(to_admin_id,status,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE care_shift_handover_item (
  handover_id BIGINT NOT NULL,
  incident_id BIGINT NOT NULL,
  PRIMARY KEY(handover_id,incident_id),
  CONSTRAINT fk_care_handover_item_header FOREIGN KEY(handover_id) REFERENCES care_shift_handover(id) ON DELETE CASCADE,
  CONSTRAINT fk_care_handover_item_incident FOREIGN KEY(incident_id) REFERENCES care_incident(id) ON DELETE RESTRICT,
  INDEX idx_care_handover_item_incident(incident_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

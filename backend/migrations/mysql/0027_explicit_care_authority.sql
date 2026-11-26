-- Snapshot the explicit authorizations before removing implicit supervisor access.
-- This migration grants no duties. Unassigned administrators remain unassigned.
CREATE TABLE care_duty_migration_inventory (
 admin_id BIGINT NOT NULL PRIMARY KEY,
 observed_duty VARCHAR(32) DEFAULT NULL,
 inspected_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 INDEX idx_care_migration_duty(observed_duty)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO care_duty_migration_inventory(admin_id,observed_duty)
SELECT u.user_id,p.duty
FROM users u LEFT JOIN care_staff_permission p ON p.admin_id=u.user_id
WHERE u.role='1';

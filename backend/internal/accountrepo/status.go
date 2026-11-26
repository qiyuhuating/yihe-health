// Package accountrepo owns persisted administrator account lifecycle state.
package accountrepo

import (
	"context"
	"database/sql"
	"errors"
)

type Account struct {
	Role   string
	Status string
}

func Lock(ctx context.Context, tx *sql.Tx, id int64) (Account, error) {
	var account Account
	err := tx.QueryRowContext(ctx,
		"SELECT COALESCE(role,''),account_status FROM users WHERE user_id=? FOR UPDATE", id).
		Scan(&account.Role, &account.Status)
	return account, err
}

func ActiveSupervisorsOtherThan(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	var count int64
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u
	 JOIN care_staff_permission p ON p.admin_id=u.user_id
	 WHERE u.role='1' AND u.account_status='active' AND p.duty='supervisor' AND u.user_id<>?`, id).Scan(&count)
	return count, err
}

func SetStatus(ctx context.Context, tx *sql.Tx, id int64, status string) error {
	result, err := tx.ExecContext(ctx, "UPDATE users SET account_status=? WHERE user_id=? AND role='1'", status, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("administrator status did not change")
	}
	return nil
}

func RetireDuty(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM care_staff_permission WHERE admin_id=?", id)
	return err
}

package securityworkflow

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"yihe-care/internal/authrepo"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

// LoginAudit runs in the factor transaction so recovery-code consumption and
// authentication audit either commit together or both roll back.
type LoginAudit func(*sql.Tx, string) error

func VerifyLogin(ctx context.Context, store *db.Database, box *securestore.Box, role string, actorID int64, otp, recoveryCode string, audit LoginAudit) (bool, string, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback()
	scope := "totp:" + role + ":" + strconv.FormatInt(actorID, 10)
	valid, generation, err := authrepo.VerifyLoginFactor(ctx, tx, box, role, actorID, scope, otp, recoveryCode, time.Now())
	if err != nil {
		return false, "", err
	}
	outcome := "factor_required"
	if valid {
		outcome = "credentials_verified"
	}
	if err := audit(tx, outcome); err != nil {
		return false, "", err
	}
	if err := tx.Commit(); err != nil {
		return false, "", err
	}
	return valid, generation, nil
}

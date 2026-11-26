package inviteworkflow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/db"
	"yihe-care/internal/inviterepo"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

type Registration struct {
	Code, Name, Phone, Password, Relation, Sex, Address string
}

type Result struct {
	FamilyID int64
	Name     string
	Resident string
}

type Failure string

const (
	CodeMissing      Failure = "code_missing"
	CodeUsed         Failure = "code_used"
	CodeExpired      Failure = "code_expired"
	ResidentMissing  Failure = "resident_missing"
	FamilyMismatch   Failure = "family_mismatch"
	RegistrationLost Failure = "registration_lost"
	CodeClaimed      Failure = "code_claimed"
)

func missing(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// Join owns the invitation lock, resident lock, account creation and code
// consumption as a single transaction. No HTTP or response types cross this boundary.
func Join(ctx context.Context, store *db.Database, box *securestore.Box, input Registration) (Result, Failure, error) {
	// Password hashing is CPU-expensive and needs no database locks.
	hash, err := auth.Hash(input.Password)
	if err != nil {
		return Result{}, "", err
	}
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Result{}, "", err
	}
	defer tx.Rollback()

	inv, err := inviterepo.LockCode(ctx, tx, input.Code)
	if missing(err) {
		return Result{}, CodeMissing, nil
	}
	if err != nil {
		return Result{}, "", err
	}
	now := time.Now()
	if inv.Used == 1 {
		return Result{}, CodeUsed, nil
	}
	if inv.ExpireTime != nil && !inv.ExpireTime.After(now) {
		return Result{}, CodeExpired, nil
	}
	lookup, err := userphone.SearchIndex(ctx, tx, box, input.Phone)
	if err != nil {
		return Result{}, "", err
	}
	dup, err := inviterepo.PhoneRegistered(ctx, tx, input.Phone, lookup)
	if err != nil {
		return Result{}, "", err
	}
	if inv.ResidentID == nil {
		return Result{}, ResidentMissing, nil
	}
	resident, err := inviterepo.LockResident(ctx, tx, *inv.ResidentID)
	if missing(err) {
		return Result{}, ResidentMissing, nil
	}
	if err != nil {
		return Result{}, "", err
	}

	var familyID int64
	if resident.FamilyID != nil {
		familyID, err = inviterepo.LockPasswordlessFamily(ctx, tx, *resident.FamilyID, input.Phone, lookup)
		if missing(err) {
			return Result{}, FamilyMismatch, nil
		}
		if err != nil {
			return Result{}, "", err
		}
	} else if dup {
		return Result{}, FamilyMismatch, nil
	}
	familyID, applied, err := inviterepo.SaveFamily(ctx, tx, box, inviterepo.FamilyRegistration{
		Name: input.Name, Phone: input.Phone, PasswordHash: hash,
		Relation: input.Relation, Sex: input.Sex, Address: input.Address,
	}, resident.FamilyID, familyID, *inv.ResidentID)
	if err != nil || !applied {
		return Result{}, RegistrationLost, nil
	}
	claimed, err := inviterepo.ConsumeCode(ctx, tx, inv.ID, familyID, time.Now())
	if err != nil || !claimed {
		return Result{}, CodeClaimed, nil
	}
	if err := tx.Commit(); err != nil {
		return Result{}, "", err
	}
	result := Result{FamilyID: familyID, Name: input.Name}
	if resident.Name != nil {
		result.Resident = *resident.Name
	}
	return result, "", nil
}

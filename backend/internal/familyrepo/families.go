// Package familyrepo owns administrative family persistence and resident bindings.
package familyrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/familyaddress"
	"yihe-care/internal/residentprofile"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

var (
	ErrPhoneInUse        = errors.New("family phone already registered")
	ErrResidentMissing   = errors.New("resident phone not found")
	ErrResidentAmbiguous = errors.New("resident phone has multiple matches")
	ErrResidentBound     = errors.New("resident already bound")
	ErrFamilyNotFound    = errors.New("family not found")
)

type Family struct {
	Name     string
	Relation string
	Sex      string
	Phone    string
	Address  string
}

type bindingTarget struct {
	id       int64
	familyID *int64
	matches  int
}

func loadBindingTarget(ctx context.Context, tx *sql.Tx, box *securestore.Box, phone string) (bindingTarget, error) {
	target := bindingTarget{}
	lookup, err := residentprofile.SearchIndex(ctx, tx, box, phone)
	if err != nil {
		return target, err
	}
	err = db.QueryEach(ctx, tx, func(rows *sql.Rows) error {
		var profile residentprofile.Record
		if err := rows.Scan(&target.id, &target.familyID, &profile.Legacy.Phone, &profile.Cipher, &profile.Lookup, &profile.Protected); err != nil {
			return err
		}
		values, err := profile.Open(box, target.id)
		if err != nil || values.Phone == nil || !residentprofile.PhoneEqual(*values.Phone, phone) {
			return errors.New("resident phone identity could not be authenticated")
		}
		target.matches++
		return nil
	}, "SELECT old_id,family_id,phone,care_profile_cipher,phone_lookup,care_profile_protected FROM oldperson WHERE "+residentprofile.PhoneMatch+" ORDER BY old_id LIMIT 2 FOR UPDATE", phone, lookup)
	return target, err
}

func phoneInUse(ctx context.Context, tx *sql.Tx, box *securestore.Box, phone string, exceptFamilyID *int64) (bool, error) {
	lookup, err := userphone.SearchIndex(ctx, tx, box, phone)
	if err != nil {
		return false, err
	}
	var used bool
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE
	    (user_phone_protected=false AND user_phone=?) OR
	    (user_phone_protected=true AND user_phone_lookup=?))
	    OR EXISTS(SELECT 1 FROM family WHERE ` + userphone.FamilyMatch + `)`
	args := []any{phone, lookup, phone, lookup}
	if exceptFamilyID != nil {
		query = `SELECT EXISTS(SELECT 1 FROM users WHERE
		    (user_phone_protected=false AND user_phone=?) OR
		    (user_phone_protected=true AND user_phone_lookup=?))
		    OR EXISTS(SELECT 1 FROM family WHERE ` + userphone.FamilyMatch + ` AND family_id<>?)`
		args = append(args, *exceptFamilyID)
	}
	err = tx.QueryRowContext(ctx, query, args...).Scan(&used)
	return used, err
}

// Create binds one unambiguous unassigned resident in a single transaction.
func Create(ctx context.Context, tx *sql.Tx, box *securestore.Box, input Family, residentPhone string) error {
	lookup, err := userphone.SearchIndex(ctx, tx, box, input.Phone)
	if err != nil {
		return err
	}
	var duplicate bool
	err = tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE
		    (user_phone_protected=false AND user_phone=?) OR
		    (user_phone_protected=true AND user_phone_lookup=?))
		    OR EXISTS(SELECT 1 FROM family WHERE `+userphone.FamilyMatch+`)`,
		input.Phone, lookup, input.Phone, lookup).Scan(&duplicate)
	if err != nil {
		return err
	}
	if duplicate {
		return ErrPhoneInUse
	}
	familyID, err := db.InsertID(ctx, tx,
		`INSERT INTO family(family_name,family_relation,family_sex) VALUES(?,?,?)`,
		input.Name, input.Relation, input.Sex)
	if err != nil {
		return err
	}
	if err := familyaddress.Store(ctx, tx, box, familyID, input.Address); err != nil {
		return err
	}
	if err := userphone.StoreFamily(ctx, tx, box, familyID, input.Phone); err != nil {
		return err
	}
	target, err := loadBindingTarget(ctx, tx, box, residentPhone)
	if err != nil {
		return err
	}
	switch {
	case target.matches == 0:
		return ErrResidentMissing
	case target.matches > 1:
		return ErrResidentAmbiguous
	case target.familyID != nil:
		return ErrResidentBound
	}
	result, err := tx.ExecContext(ctx,
		"UPDATE oldperson SET family_id=? WHERE old_id=? AND family_id IS NULL",
		familyID, target.id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrResidentBound
	}
	return nil
}

func Update(ctx context.Context, tx *sql.Tx, box *securestore.Box, familyID int64, input Family) error {
	used, err := phoneInUse(ctx, tx, box, input.Phone, &familyID)
	if err != nil {
		return err
	}
	if used {
		return ErrPhoneInUse
	}
	address, err := familyaddress.PrepareUpdate(ctx, tx, box, familyID, input.Address)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrFamilyNotFound
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE family SET family_name=?,family_relation=?,family_sex=?,
		 family_address=?,family_address_cipher=?,family_address_protected=? WHERE family_id=?`,
		input.Name, input.Relation, input.Sex, address.Legacy, address.Cipher, address.Protected, familyID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrFamilyNotFound
	}
	return userphone.StoreFamily(ctx, tx, box, familyID, input.Phone)
}

// Delete clears the resident binding and database sessions atomically.
// Caller revokes non-database sessions only after the transaction commits.
func Delete(ctx context.Context, tx *sql.Tx, familyID int64) error {
	if _, err := tx.ExecContext(ctx,
		"UPDATE oldperson SET family_id=NULL WHERE family_id=?", familyID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM family WHERE family_id=?", familyID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrFamilyNotFound
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM web_session WHERE role='FAMILY' AND actor_id=?", familyID); err != nil {
		return err
	}
	return nil
}

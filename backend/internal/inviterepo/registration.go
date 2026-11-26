// Package inviterepo persists invitations and their family registrations.
package inviterepo

import (
	"context"
	"database/sql"

	"yihe-care/internal/db"
	"yihe-care/internal/familyaddress"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

type FamilyRegistration struct {
	Name         string
	Phone        string
	PasswordHash string
	Relation     string
	Sex          string
	Address      string
}

// SaveFamily inserts a new family or activates only its existing passwordless
// placeholder. The caller owns the invitation/resident row locks and commit.
func SaveFamily(ctx context.Context, tx *sql.Tx, box *securestore.Box, registration FamilyRegistration, boundFamilyID *int64, familyID, residentID int64) (int64, bool, error) {
	var err error
	if boundFamilyID == nil {
		familyID, err = db.InsertID(ctx, tx,
			`INSERT INTO family(family_name,family_password,family_relation,family_sex)
			 VALUES(?,?,?,?)`,
			registration.Name, registration.PasswordHash,
			registration.Relation, registration.Sex)
		if err == nil {
			err = familyaddress.Store(ctx, tx, box, familyID, registration.Address)
		}
	} else {
		address, err := familyaddress.PrepareUpdate(ctx, tx, box, familyID, registration.Address)
		if err != nil {
			return familyID, false, err
		}
		result, updateErr := tx.ExecContext(ctx,
			`UPDATE family SET family_name=?,family_password=?,
			 family_relation=?,family_sex=?,family_address=?,family_address_cipher=?,family_address_protected=?
			 WHERE family_id=? AND family_password IS NULL`,
			registration.Name, registration.PasswordHash, registration.Relation,
			registration.Sex, address.Legacy, address.Cipher, address.Protected, familyID)
		if updateErr != nil {
			return familyID, false, updateErr
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return familyID, false, err
		}
		if affected != 1 {
			return familyID, false, nil
		}
	}
	if err != nil {
		return familyID, false, err
	}
	if err := userphone.StoreFamily(ctx, tx, box, familyID, registration.Phone); err != nil {
		return familyID, false, err
	}
	if boundFamilyID == nil {
		result, err := tx.ExecContext(ctx,
			"UPDATE oldperson SET family_id=? WHERE old_id=? AND family_id IS NULL",
			familyID, residentID)
		if err != nil {
			return familyID, false, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return familyID, false, err
		}
		if changed != 1 {
			return familyID, false, nil
		}
	}
	return familyID, true, nil
}

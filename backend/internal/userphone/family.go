package userphone

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
)

const FamilyMatch = "((family_phone_protected=false AND family_phone=?) OR (family_phone_protected=true AND family_phone_lookup=?))"
const QualifiedFamilyMatch = "((f.family_phone_protected=false AND f.family_phone=?) OR (f.family_phone_protected=true AND f.family_phone_lookup=?))"

// StoreFamily shares the caller's transaction with the profile/registration.
// Once any account is protected, a missing key cannot downgrade new writes.
func StoreFamily(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, phone string) error {
	if id < 1 || !auth.ValidPhone(phone) {
		return errors.New("invalid family phone write")
	}
	if _, err := SearchIndex(ctx, tx, box, phone); err != nil {
		return err
	}
	var result sql.Result
	var err error
	if box == nil {
		result, err = tx.ExecContext(ctx, "UPDATE family SET family_phone=? WHERE family_id=? AND family_phone_protected=false", phone, id)
	} else {
		if err := BindLookupKey(ctx, tx, box); err != nil {
			return err
		}
		value, sealErr := ProtectFamily(box, id, phone)
		if sealErr != nil {
			return sealErr
		}
		result, err = tx.ExecContext(ctx, `UPDATE family SET family_phone=NULL,
 family_phone_cipher=?,family_phone_lookup=?,family_phone_protected=true WHERE family_id=?`, value.Ciphertext, value.Lookup, id)
	}
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return errors.New("family phone write affected unexpected rows")
	}
	return nil
}

package familyrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/familyaddress"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

// LockProfile prevents deletion, phone reassignment and profile writes from
// passing a family account identity check concurrently.
func LockProfile(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, phone string) (int64, string, error) {
	var familyID int64
	var record userphone.Record
	var err error
	if id > 0 {
		err = tx.QueryRowContext(ctx,
			"SELECT family_id,family_phone,family_phone_cipher,family_phone_lookup,family_phone_protected FROM family WHERE family_id=? FOR UPDATE", id).Scan(&familyID, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected)
	} else {
		lookup, lookupErr := userphone.SearchIndex(ctx, tx, box, phone)
		if lookupErr != nil {
			return 0, "", lookupErr
		}
		err = tx.QueryRowContext(ctx,
			"SELECT family_id,family_phone,family_phone_cipher,family_phone_lookup,family_phone_protected FROM family WHERE "+userphone.FamilyMatch+" FOR UPDATE", phone, lookup).Scan(&familyID, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrFamilyNotFound
	}
	if err != nil {
		return 0, "", err
	}
	opened, err := record.OpenFamilyStored(box, familyID)
	if err != nil || opened == nil {
		return familyID, "", err
	}
	return familyID, *opened, nil
}

func UpdateProfileDetails(ctx context.Context, tx *sql.Tx, box *securestore.Box, familyID int64, oldPhone, name, sex, address, phone string) (bool, error) {
	if oldPhone != phone {
		used, err := phoneInUse(ctx, tx, box, phone, &familyID)
		if err != nil {
			return false, err
		}
		if used {
			return false, ErrPhoneInUse
		}
	}
	protected, err := familyaddress.PrepareUpdate(ctx, tx, box, familyID, address)
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx,
		"UPDATE family SET family_name=?,family_sex=?,family_address=?,family_address_cipher=?,family_address_protected=? WHERE family_id=?",
		name, sex, protected.Legacy, protected.Cipher, protected.Protected, familyID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if err := userphone.StoreFamily(ctx, tx, box, familyID, phone); err != nil {
		return false, err
	}
	return true, nil
}

func UpdateAvatar(ctx context.Context, tx *sql.Tx, familyID int64, image string) (bool, error) {
	result, err := tx.ExecContext(ctx, "UPDATE family SET family_tou_pic=? WHERE family_id=?", image, familyID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func PasswordByPhone(ctx context.Context, store db.RowQuerier, box *securestore.Box, phone string) (*string, error) {
	var hash *string
	lookup, err := userphone.SearchIndex(ctx, store, box, phone)
	if err != nil {
		return nil, err
	}
	err = store.QueryRowContext(ctx, "SELECT family_password FROM family WHERE "+userphone.FamilyMatch, phone, lookup).Scan(&hash)
	return hash, err
}

func LockedPassword(ctx context.Context, tx *sql.Tx, familyID int64) (*string, error) {
	var hash *string
	err := tx.QueryRowContext(ctx, "SELECT family_password FROM family WHERE family_id=?", familyID).Scan(&hash)
	return hash, err
}

func SetPassword(ctx context.Context, tx *sql.Tx, familyID int64, hash string) error {
	_, err := tx.ExecContext(ctx, "UPDATE family SET family_password=? WHERE family_id=?", hash, familyID)
	return err
}

package authrepo

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"yihe-care/internal/auth"
	"yihe-care/internal/carerepo"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

var ErrAlreadyInitialized = errors.New("administrator already initialized")
var ErrInvalidAdmin = errors.New("invalid administrator input")

// InitializeAdmin serializes first-run account creation using the metadata lock.
func InitializeAdmin(ctx context.Context, store *db.Database, box *securestore.Box, name, phone, password string) error {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var metadataID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM app_metadata WHERE id=1 FOR UPDATE").Scan(&metadataID); err != nil {
		return err
	}
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrAlreadyInitialized
	}
	if !auth.ValidPhone(phone) || strings.TrimSpace(name) == "" ||
		utf8.RuneCountInString(name) > 10 || !auth.ValidNewPassword(password) {
		return ErrInvalidAdmin
	}
	hashed, err := auth.Hash(password)
	if err != nil {
		return err
	}
	var adminID int64
	if box == nil {
		adminID, err = db.InsertID(ctx, tx,
			"INSERT INTO users(user_name,user_phone,password,role) VALUES(?,?,?,'1')",
			name, phone, hashed)
		if err != nil {
			return err
		}
	} else {
		id, err := db.InsertID(ctx, tx,
			"INSERT INTO users(user_name,password,role) VALUES(?,?,'1')",
			name, hashed)
		if err != nil {
			return err
		}
		adminID = id
		protected, err := userphone.Protect(box, id, phone)
		if err != nil {
			return err
		}
		if err := userphone.BindLookupKey(ctx, tx, box); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE users SET user_phone_cipher=?,user_phone_lookup=?,user_phone_protected=true WHERE user_id=?",
			protected.Ciphertext, protected.Lookup, id); err != nil {
			return err
		}
	}
	// First-run administrator and the first explicit supervisor are one
	// atomic identity/authorization/audit operation.
	if err := carerepo.SetDuty(ctx, tx, adminID, adminID, "supervisor"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO care_access_audit(admin_id,action,subject_admin_id) VALUES(?,'supervisor_initialized',?)",
		adminID, adminID); err != nil {
		return err
	}
	return tx.Commit()
}

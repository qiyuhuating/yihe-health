// Package authrepo owns credential lookup and password verification.
package authrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/auth"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
	"yihe-care/internal/userphone"
)

// Match is the credential lookup result. Unlinked identifies an authenticated
// family account without a resident, which must not create a login session.
type Match struct {
	Principal     session.Principal
	Authenticated bool
	Unlinked      bool
}

var accounts = [...]struct{ role, query string }{
	{auth.RoleAdmin, `SELECT user_id,user_name,user_phone,password,NULL,user_phone_cipher,user_phone_lookup,user_phone_protected
 FROM users WHERE account_status='active' AND ((user_phone_protected=false AND user_phone=?) OR (user_phone_protected=true AND user_phone_lookup=?))`},
	{auth.RoleFamily, `SELECT f.family_id,f.family_name,f.family_phone,f.family_password,o.old_id,f.family_phone_cipher,f.family_phone_lookup,f.family_phone_protected
 FROM family f LEFT JOIN oldperson o ON o.family_id=f.family_id
 WHERE ` + userphone.QualifiedFamilyMatch + ` ORDER BY o.old_id LIMIT 1`},
}

// An invalid bcrypt hash with the same cost as real account hashes prevents
// account-presence timing differences when neither table has a matching phone.
const dummyHash = "$2a$12$S1k/y1BsWtNAy/eeN.zaleT.hNjDpyRQ/1S7MCXu5Y8GN61EoKbMK"

// Authenticate keeps admin-first role selection and checks each applicable
// stored hash before deciding whether the credentials are valid.
func Authenticate(ctx context.Context, store *db.Database, box *securestore.Box, phone, password string) (Match, error) {
	lookup, err := userphone.SearchIndex(ctx, store, box, phone)
	if err != nil {
		return Match{}, err
	}
	compared := false
	for _, account := range accounts {
		var id int64
		var name, storedPhone, hash sql.NullString
		var residentID sql.NullInt64
		var protected userphone.Record
		args := []any{phone, lookup}
		err := store.QueryRowContext(ctx, account.query, args...).Scan(&id, &name, &storedPhone, &hash, &residentID,
			&protected.Cipher, &protected.Lookup, &protected.Protected)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return Match{}, err
		}
		if account.role == auth.RoleAdmin || account.role == auth.RoleFamily {
			if storedPhone.Valid {
				protected.Legacy = &storedPhone.String
			}
			var opened *string
			var err error
			if account.role == auth.RoleAdmin {
				opened, err = protected.OpenStored(box, id)
			} else {
				opened, err = protected.OpenFamilyStored(box, id)
			}
			if err != nil {
				return Match{}, err
			}
			storedPhone = sql.NullString{}
			if opened != nil {
				storedPhone = sql.NullString{String: *opened, Valid: true}
			}
		}
		compared = true
		if !auth.Matches(password, hash.String) {
			continue
		}
		if account.role == auth.RoleFamily && !residentID.Valid {
			return Match{Unlinked: true}, nil
		}
		return Match{
			Authenticated: true,
			Principal: session.Principal{
				Role: account.role, ID: id, Phone: storedPhone.String,
				OldID: residentID.Int64, DisplayName: name.String,
			},
		}, nil
	}
	if !compared {
		_ = auth.Matches(password, dummyHash)
	}
	return Match{}, nil
}

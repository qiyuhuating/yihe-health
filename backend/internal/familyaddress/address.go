// Package familyaddress protects family addresses without changing phone lookup or access rules.
package familyaddress

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"unicode/utf8"

	"yihe-care/internal/securestore"
)

const Columns = "family_address,family_address_cipher,family_address_protected"

type Record struct {
	Legacy, Cipher *string
	Protected      bool
}

func Scope(id string) string { return "family-address:" + id }

func valid(address string) bool {
	return utf8.ValidString(address) && utf8.RuneCountInString(address) <= 200
}

func (r Record) Open(box *securestore.Box, id int64) (*string, error) {
	if !r.Protected {
		if r.Cipher != nil || (r.Legacy != nil && !valid(*r.Legacy)) {
			return nil, errors.New("invalid legacy family address")
		}
		return r.Legacy, nil
	}
	if id < 1 || r.Legacy != nil || r.Cipher == nil || len(*r.Cipher) > 2048 {
		return nil, errors.New("invalid protected family address")
	}
	address, err := box.Open(*r.Cipher, Scope(strconv.FormatInt(id, 10)))
	if err != nil || !valid(address) {
		return nil, errors.New("family address authentication failed")
	}
	return &address, nil
}

func encode(box *securestore.Box, id int64, address string) (Record, error) {
	if id < 1 || !valid(address) {
		return Record{}, errors.New("invalid family address")
	}
	if box == nil {
		return Record{Legacy: &address}, nil
	}
	cipher, err := box.Seal(address, Scope(strconv.FormatInt(id, 10)))
	if err != nil {
		return Record{}, err
	}
	return Record{Cipher: &cipher, Protected: true}, nil
}

// PrepareUpdate authenticates the existing address under the caller's row lock
// before any account fields are changed.
func PrepareUpdate(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, address string) (Record, error) {
	var current Record
	err := tx.QueryRowContext(ctx, "SELECT "+Columns+" FROM family WHERE family_id=? FOR UPDATE", id).
		Scan(&current.Legacy, &current.Cipher, &current.Protected)
	if err != nil {
		return Record{}, err
	}
	if _, err := current.Open(box, id); err != nil {
		return Record{}, err
	}
	return encode(box, id, address)
}

// Store is used for newly reserved IDs and verified, locked backfill rows.
func Store(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, address string) error {
	record, err := encode(box, id, address)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE family SET family_address=?,family_address_cipher=?,family_address_protected=? WHERE family_id=?",
		record.Legacy, record.Cipher, record.Protected, id)
	if err != nil {
		return err
	}
	if matched, err := result.RowsAffected(); err != nil || matched != 1 {
		return errors.New("family address write did not match exactly one row")
	}
	return nil
}

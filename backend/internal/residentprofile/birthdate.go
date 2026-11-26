package residentprofile

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"yihe-care/internal/securestore"
)

// BirthdateRecord is separate from the care profile so readers that only need
// contact information do not load or decrypt the date of birth.
type BirthdateRecord struct {
	Legacy    *time.Time
	Cipher    *string
	Protected bool
}

func BirthdateScope(id string) string { return "resident-birthdate:" + id }

func (r BirthdateRecord) Open(box *securestore.Box, id int64) (*time.Time, error) {
	if !r.Protected {
		if r.Cipher != nil {
			return nil, errors.New("invalid legacy resident birthdate")
		}
		return r.Legacy, nil
	}
	if id < 1 || r.Legacy != nil || r.Cipher == nil || len(*r.Cipher) > 256 {
		return nil, errors.New("invalid protected resident birthdate")
	}
	plain, err := box.Open(*r.Cipher, BirthdateScope(strconv.FormatInt(id, 10)))
	if err != nil {
		return nil, errors.New("resident birthdate authentication failed")
	}
	date, err := parseBirthdate(plain)
	if err != nil {
		return nil, errors.New("invalid protected resident birthdate")
	}
	return &date, nil
}

// StoreBirthdate writes to an already reserved or locked row. The caller must
// authenticate the old value before replacing it.
func StoreBirthdate(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, date time.Time) error {
	if id < 1 || date.Year() < 1 || date.Year() > 9999 {
		return errors.New("invalid resident birthdate")
	}
	plain := date.Format(time.DateOnly)
	var legacy, ciphertext *string
	if box == nil {
		legacy = &plain
	} else {
		sealed, err := box.Seal(plain, BirthdateScope(strconv.FormatInt(id, 10)))
		if err != nil {
			return err
		}
		ciphertext = &sealed
	}
	result, err := tx.ExecContext(ctx, `UPDATE oldperson
 SET bothdate=?,birthdate_cipher=?,birthdate_protected=? WHERE old_id=?`, legacy, ciphertext, box != nil, id)
	if err != nil {
		return err
	}
	if matched, err := result.RowsAffected(); err != nil || matched != 1 {
		return errors.New("resident birthdate write did not match exactly one row")
	}
	return nil
}

func parseBirthdate(value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil || date.Year() < 1 || date.Format(time.DateOnly) != value {
		return time.Time{}, errors.New("invalid resident birthdate")
	}
	return date, nil
}

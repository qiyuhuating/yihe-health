package residentprofile

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

type Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const PhoneMatch = `((care_profile_protected=false AND phone=?) OR (care_profile_protected=true AND phone_lookup=?))`

func fingerprint(box *securestore.Box) string {
	return box.Index("resident-profile-key-binding", "v1-pad-space")
}

func VerifyKey(ctx context.Context, q Queryer, box *securestore.Box) error {
	var binding *string
	var protected bool
	if err := q.QueryRowContext(ctx, `SELECT binding,CASE WHEN binding IS NULL THEN
 EXISTS(SELECT 1 FROM oldperson WHERE care_profile_protected=true) ELSE false END
 FROM (SELECT (SELECT lookup_verifier FROM resident_profile_key_state WHERE singleton=1) AS binding) metadata`).Scan(&binding, &protected); err != nil {
		return errors.New("cannot verify resident profile lookup key")
	}
	if box == nil {
		if binding != nil || protected {
			return errors.New("resident profile key is unavailable")
		}
		return nil
	}
	if protected && binding == nil {
		return errors.New("resident profile key binding is missing")
	}
	if binding != nil && subtle.ConstantTimeCompare([]byte(*binding), []byte(fingerprint(box))) != 1 {
		return errors.New("resident profile lookup key mismatch")
	}
	return nil
}

func SearchIndex(ctx context.Context, q Queryer, box *securestore.Box, phone string) (string, error) {
	if err := VerifyKey(ctx, q, box); err != nil {
		return "", err
	}
	if box == nil {
		return "", nil
	}
	return phoneIndex(box, phone), nil
}

func bindKey(ctx context.Context, tx *sql.Tx, box *securestore.Box) error {
	if err := VerifyKey(ctx, tx, box); err != nil || box == nil {
		return err
	}
	var actual string
	err := tx.QueryRowContext(ctx, "SELECT lookup_verifier FROM resident_profile_key_state WHERE singleton=1 FOR SHARE").Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO resident_profile_key_state(singleton,lookup_verifier) VALUES(1,?)
 ON DUPLICATE KEY UPDATE singleton=singleton`, fingerprint(box)); err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, "SELECT lookup_verifier FROM resident_profile_key_state WHERE singleton=1 FOR SHARE").Scan(&actual)
	}
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(actual), []byte(fingerprint(box))) != 1 {
		return errors.New("resident profile binding mismatch")
	}
	return nil
}

// PrepareWrite obtains the binding before any resident row locks. Backfill and
// online writers must use this same order, including the very first binding.
func PrepareWrite(ctx context.Context, tx *sql.Tx, box *securestore.Box) error {
	return bindKey(ctx, tx, box)
}

// Store updates an already locked/reserved resident inside the caller's original
// business transaction. It never downgrades existing protected data without keys.
func Store(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, values Values) error {
	if id < 1 || !values.valid() {
		return errors.New("invalid resident profile values")
	}
	if err := bindKey(ctx, tx, box); err != nil {
		return err
	}
	stored := Record{Legacy: values}
	if box != nil {
		var err error
		stored, err = Protect(box, id, values)
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE oldperson SET phone=?,room=?,illness=?,warn=?,food_like=?,food_hate=?,drag=?,
 care_profile_cipher=?,phone_lookup=?,care_profile_protected=? WHERE old_id=?`,
		stored.Legacy.Phone, stored.Legacy.Room, stored.Legacy.Illness, stored.Legacy.Warn, stored.Legacy.FoodLike, stored.Legacy.FoodHate, stored.Legacy.Drag,
		stored.Cipher, stored.Lookup, stored.Protected, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.New("resident profile update did not match exactly one row")
	}
	return nil
}

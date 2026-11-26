package guardiancontact

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

const keyScope = "guardian-contact-key-state"

func fingerprint(box *securestore.Box) string {
	return box.Index("guardian-contact-key-binding", "v1")
}

// The encrypted marker verifies an AEAD reader as well as the independent
// binding key. It is itself a rekey target and never contains actual contacts.
func verifyBinding(box *securestore.Box, binding, cipher string) error {
	if box == nil || subtle.ConstantTimeCompare([]byte(binding), []byte(fingerprint(box))) != 1 {
		return errors.New("guardian contact key binding mismatch")
	}
	plain, err := box.Open(cipher, keyScope)
	if err != nil || plain != "guardian-contact:v1:"+binding {
		return errors.New("guardian contact reader key mismatch")
	}
	return nil
}

func verifyKeyState(ctx context.Context, q db.RowQuerier, box *securestore.Box) (bool, error) {
	var binding, cipher *string
	var protected bool
	if err := q.QueryRowContext(ctx, `SELECT lookup_verifier,key_verifier,
 CASE WHEN lookup_verifier IS NULL THEN EXISTS(SELECT 1 FROM customer WHERE contact_protected=true) ELSE false END
 FROM (SELECT 1 AS singleton) expected LEFT JOIN guardian_contact_key_state USING(singleton)`).Scan(&binding, &cipher, &protected); err != nil {
		return false, errors.New("cannot verify guardian contact keys")
	}
	if binding == nil && cipher == nil {
		if protected {
			return false, errors.New("guardian contact key binding is missing")
		}
		return false, nil
	}
	if binding == nil || cipher == nil {
		return false, errors.New("inconsistent guardian contact key state")
	}
	return true, verifyBinding(box, *binding, *cipher)
}

func VerifyKey(ctx context.Context, q db.RowQuerier, box *securestore.Box) error {
	_, err := verifyKeyState(ctx, q, box)
	return err
}

// PrepareWrite locks key metadata before the customer row, matching backfill.
func PrepareWrite(ctx context.Context, tx *sql.Tx, box *securestore.Box) error {
	bound, err := verifyKeyState(ctx, tx, box)
	if err != nil || box == nil {
		return err
	}
	var binding, cipher string
	if !bound {
		// Do not take a shared gap lock before the first INSERT: two initial
		// writers would otherwise deadlock while upgrading that missing key.
		binding = fingerprint(box)
		cipher, err = box.Seal("guardian-contact:v1:"+binding, keyScope)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO guardian_contact_key_state(singleton,lookup_verifier,key_verifier) VALUES(1,?,?)
 ON DUPLICATE KEY UPDATE singleton=singleton`, binding, cipher); err != nil {
			return err
		}
	}
	err = tx.QueryRowContext(ctx, "SELECT lookup_verifier,key_verifier FROM guardian_contact_key_state WHERE singleton=1 FOR SHARE").Scan(&binding, &cipher)
	if err != nil {
		return err
	}
	return verifyBinding(box, binding, cipher)
}

// Store operates inside the caller's original transaction after reserving or
// locking the customer row. Existing protected values must be authenticated by
// the updater before any mutation; new records reserve IDs without contacts.
func Store(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, values Values) error {
	if id < 1 || !values.valid() {
		return errors.New("invalid guardian contact values")
	}
	if err := PrepareWrite(ctx, tx, box); err != nil {
		return err
	}
	record := Record{Legacy: values}
	if box != nil {
		var err error
		record, err = Protect(box, id, values)
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE customer SET customer_phone=?,customer_address=?,contact_cipher=?,contact_protected=? WHERE customer_id=?`,
		record.Legacy.Phone, record.Legacy.Address, record.Cipher, record.Protected, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errors.New("guardian contact update did not match exactly one row")
	}
	return nil
}

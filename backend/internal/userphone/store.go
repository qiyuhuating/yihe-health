package userphone

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

type Record struct {
	Legacy, Cipher, Lookup *string
	Protected              bool
}

// OpenStored rejects inconsistent or tampered protected rows, never falling
// back to a legacy phone when an encrypted row fails authentication.
func (r Record) OpenStored(box *securestore.Box, id int64) (*string, error) {
	return r.openStored(box, id, Reveal)
}

func (r Record) OpenFamilyStored(box *securestore.Box, id int64) (*string, error) {
	return r.openStored(box, id, RevealFamily)
}

func (r Record) openStored(box *securestore.Box, id int64, open func(*securestore.Box, int64, Protected) (string, error)) (*string, error) {
	if !r.Protected {
		if r.Cipher != nil || r.Lookup != nil {
			return nil, errors.New("inconsistent legacy account phone")
		}
		return r.Legacy, nil
	}
	if r.Legacy != nil || (r.Cipher == nil) != (r.Lookup == nil) {
		return nil, errors.New("inconsistent protected account phone")
	}
	if r.Cipher == nil {
		return nil, nil
	}
	phone, err := open(box, id, Protected{Ciphertext: *r.Cipher, Lookup: *r.Lookup})
	if err != nil {
		return nil, err
	}
	return &phone, nil
}

type Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// SearchIndex validates lookup-key binding and detects keyless protected rows.
// Legacy-only installations may authenticate during a staged migration.
func SearchIndex(ctx context.Context, q Queryer, box *securestore.Box, phone string) (string, error) {
	if err := VerifyLookupKey(ctx, q, box); err != nil {
		return "", err
	}
	if box == nil {
		return "", nil
	}
	return Lookup(box, phone)
}

func VerifyLookupKey(ctx context.Context, q Queryer, box *securestore.Box) error {
	var verifier *string
	var protected bool
	err := q.QueryRowContext(ctx, `SELECT (SELECT lookup_verifier FROM account_phone_key_state WHERE singleton=1),
 (EXISTS(SELECT 1 FROM users WHERE user_phone_protected=true) OR
 EXISTS(SELECT 1 FROM family WHERE family_phone_protected=true))`).Scan(&verifier, &protected)
	if err != nil {
		return errors.New("cannot verify account phone lookup key")
	}
	if box == nil {
		if protected || verifier != nil {
			return errors.New("account phone lookup key is unavailable")
		}
		return nil
	}
	fingerprint, err := KeyBinding(box)
	if err != nil {
		return err
	}
	if verifier == nil && protected {
		return errors.New("account phone key binding is missing")
	}
	if verifier != nil && subtle.ConstantTimeCompare([]byte(fingerprint), []byte(*verifier)) != 1 {
		return errors.New("account phone lookup key differs from stored binding")
	}
	return nil
}

// BindLookupKey registers the immutable fingerprint in the same business
// transaction as the first encrypted write. A mismatched key always fails.
func BindLookupKey(ctx context.Context, tx *sql.Tx, box *securestore.Box) error {
	if err := VerifyLookupKey(ctx, tx, box); err != nil {
		return err
	}
	fingerprint, err := KeyBinding(box)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_phone_key_state(singleton,lookup_verifier)
 VALUES(1,?) ON DUPLICATE KEY UPDATE singleton=singleton`, fingerprint); err != nil {
		return errors.New("cannot register account phone lookup key")
	}
	var actual string
	if err := tx.QueryRowContext(ctx, `SELECT lookup_verifier FROM account_phone_key_state WHERE singleton=1`).Scan(&actual); err != nil {
		return errors.New("cannot read account phone lookup key binding")
	}
	if subtle.ConstantTimeCompare([]byte(fingerprint), []byte(actual)) != 1 {
		return errors.New("account phone lookup key binding mismatch")
	}
	return nil
}

package socialrepo

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

const likePhoneMatch = `((phone_protected=false AND phone=?) OR (phone_protected=true AND phone_lookup=?))`

func commentPhoneScope(id int64) string { return "social-comment-phone:" + strconv.FormatInt(id, 10) }

func sealCommentPhone(box *securestore.Box, id int64, phone string) (string, error) {
	if box == nil || id <= 0 || !auth.ValidPhone(phone) {
		return "", errors.New("invalid comment phone protection input")
	}
	return box.Seal(phone, commentPhoneScope(id))
}

func openCommentPhone(box *securestore.Box, id int64, legacy, cipher *string, protected bool) (*string, error) {
	if !protected {
		if cipher != nil {
			return nil, errors.New("inconsistent legacy comment phone")
		}
		return legacy, nil
	}
	if box == nil || id <= 0 || legacy != nil || cipher == nil || !strings.HasPrefix(*cipher, securestore.Prefix) {
		return nil, errors.New("invalid protected comment phone")
	}
	phone, err := box.Open(*cipher, commentPhoneScope(id))
	if err != nil || !auth.ValidPhone(phone) {
		return nil, errors.New("comment phone authentication failed")
	}
	return &phone, nil
}

func socialPhoneFingerprint(box *securestore.Box) string {
	return box.Index("social-phone-key-binding", "v1")
}

// Verify the social ledger itself, even after all account records are removed.
// Missing metadata on existing protected rows must never create a new binding.
func verifySocialPhoneKey(ctx context.Context, q userphone.Queryer, box *securestore.Box) error {
	var binding *string
	var protected bool
	// With a ledger, its fingerprint is sufficient. Only probe social rows when
	// metadata is missing, avoiding a legacy-table scan on every normal request.
	if err := q.QueryRowContext(ctx, `SELECT binding, CASE WHEN binding IS NULL THEN
 (EXISTS(SELECT 1 FROM said WHERE said_phone_protected=true) OR
 EXISTS(SELECT 1 FROM nice_count WHERE phone_protected=true)) ELSE false END
 FROM (SELECT (SELECT lookup_verifier FROM social_phone_key_state WHERE singleton=1) AS binding) metadata`).Scan(&binding, &protected); err != nil {
		return errors.New("cannot verify social phone lookup key")
	}
	if box == nil {
		if binding != nil || protected {
			return errors.New("social phone key is unavailable")
		}
		return nil
	}
	if binding == nil && protected {
		return errors.New("social phone key binding is missing")
	}
	if binding != nil && subtle.ConstantTimeCompare([]byte(*binding), []byte(socialPhoneFingerprint(box))) != 1 {
		return errors.New("social phone lookup key mismatch")
	}
	return nil
}

func socialPhoneIndex(ctx context.Context, q userphone.Queryer, box *securestore.Box, phone string) (string, error) {
	if err := verifySocialPhoneKey(ctx, q, box); err != nil {
		return "", err
	}
	if box == nil {
		return "", nil
	}
	return box.Index("social-like-phone", phone), nil
}

func bindSocialPhoneKey(ctx context.Context, tx *sql.Tx, box *securestore.Box, phone string) (string, error) {
	if !auth.ValidPhone(phone) {
		return "", errors.New("invalid social phone")
	}
	lookup, err := socialPhoneIndex(ctx, tx, box, phone)
	if err != nil || box == nil {
		return lookup, err
	}
	var actual string
	err = tx.QueryRowContext(ctx, "SELECT lookup_verifier FROM social_phone_key_state WHERE singleton=1 FOR SHARE").Scan(&actual)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO social_phone_key_state(singleton,lookup_verifier) VALUES(1,?)
 ON DUPLICATE KEY UPDATE singleton=singleton`, socialPhoneFingerprint(box)); err != nil {
			return "", err
		}
		err = tx.QueryRowContext(ctx, "SELECT lookup_verifier FROM social_phone_key_state WHERE singleton=1 FOR SHARE").Scan(&actual)
	}
	if err != nil {
		return "", err
	}
	if subtle.ConstantTimeCompare([]byte(actual), []byte(socialPhoneFingerprint(box))) != 1 {
		return "", errors.New("social phone binding mismatch")
	}
	return lookup, nil
}

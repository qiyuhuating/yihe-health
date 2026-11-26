package authrepo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"yihe-care/internal/securestore"
)

// CreateFactorSetup generates an unshared TOTP seed and persists only its
// encrypted representation in the caller's authenticated transaction.
func CreateFactorSetup(ctx context.Context, tx *sql.Tx, box *securestore.Box, role string, actorID int64, scope string, now time.Time) (map[string]any, error) {
	var seed [20]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, err
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(seed[:])
	sealed, err := box.Seal(secret, scope)
	if err != nil {
		return nil, err
	}
	if err := SetupFactor(ctx, tx, role, actorID, sealed, now.Add(10*time.Minute), uuid.NewString()); err != nil {
		return nil, err
	}
	uri := "otpauth://totp/" + url.PathEscape("颐和:"+role+":"+strconv.FormatInt(actorID, 10)) +
		"?secret=" + secret + "&issuer=" + url.QueryEscape("颐和") + "&algorithm=SHA1&digits=6&period=30"
	return map[string]any{"secret": secret, "uri": uri}, nil
}

// GenerateRecoveryCodes returns eight one-time codes with SHA-256 hashes.
// Callers must expose cleartext codes only after the transaction commits.
func GenerateRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 0, 8)
	hashes := make([]string, 0, 8)
	for range 8 {
		var bytes [12]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return nil, nil, err
		}
		code := hex.EncodeToString(bytes[:])
		digest := sha256.Sum256([]byte(code))
		codes = append(codes, code)
		hashes = append(hashes, hex.EncodeToString(digest[:]))
	}
	return codes, hashes, nil
}

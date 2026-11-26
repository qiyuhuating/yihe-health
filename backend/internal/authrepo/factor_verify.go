package authrepo

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"yihe-care/internal/securestore"
)

var ErrFactorSecretUnavailable = errors.New("MFA encryption key unavailable")

// VerifyLoginFactor consumes TOTP counters or one-time recovery codes inside
// the same transaction as login auditing. The caller commits or rolls back.
func VerifyLoginFactor(ctx context.Context, tx *sql.Tx, box *securestore.Box, role string, actorID int64, scope, otp, recoveryCode string, now time.Time) (bool, string, error) {
	factor, err := LockLoginFactor(ctx, tx, role, actorID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if !factor.Enabled {
		return true, "", nil
	}
	secret, err := box.Open(factor.Sealed, scope)
	if err != nil {
		return false, "", ErrFactorSecretUnavailable
	}
	counter, valid := securestore.VerifyTOTP(secret, otp, now, factor.LastCounter)
	if !valid && recoveryCode != "" {
		var hashes []string
		if err := json.Unmarshal(factor.RecoveryHashes, &hashes); err != nil {
			return false, "", err
		}
		digest := sha256.Sum256([]byte(recoveryCode))
		candidate := hex.EncodeToString(digest[:])
		for index, hash := range hashes {
			if subtle.ConstantTimeCompare([]byte(hash), []byte(candidate)) == 1 {
				hashes = append(hashes[:index], hashes[index+1:]...)
				factor.RecoveryHashes, err = json.Marshal(hashes)
				if err != nil {
					return false, "", err
				}
				counter, valid = factor.LastCounter, true
				break
			}
		}
	}
	if !valid {
		return false, "", nil
	}
	if err := UpdateLoginFactor(ctx, tx, role, actorID, counter, factor.RecoveryHashes); err != nil {
		return false, "", err
	}
	return true, factor.Generation, nil
}

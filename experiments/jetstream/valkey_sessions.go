package lane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/valkey-io/valkey-go"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

var ErrSessionNotAuthorized = errors.New("session missing, expired or revoked")
var ErrSessionShadowIntegrity = errors.New("session shadow integrity check failed")

// SessionShadow moves encrypted session payloads into Valkey while retaining
// an authoritative SQL metadata fence. The fixture installs the metadata
// projection transactionally with the existing web_session mutations.
// It is never a cache-only authorization path.
type SessionShadow struct {
	DB    *sql.DB
	Box   *securestore.Box
	Cache valkey.Client
}

type sessionMeta struct {
	role    string
	actorID int64
	expires time.Time
	digest  []byte
	factor  sql.NullString
}

func sessionHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func sessionCacheKey(hash string) string { return "yihe:shadow:session:" + hash }

func (s SessionShadow) authenticate(hash, sealed string, meta sessionMeta) (*session.Session, error) {
	sum := sha256.Sum256([]byte(sealed))
	if !bytes.Equal(sum[:], meta.digest) || s.Box == nil {
		return nil, ErrSessionShadowIntegrity
	}
	plain, err := s.Box.OpenBytes(sealed, "session")
	if err != nil {
		return nil, ErrSessionShadowIntegrity
	}
	var sess session.Session
	if err = json.Unmarshal(plain, &sess); err != nil {
		return nil, ErrSessionShadowIntegrity
	}
	if sessionHash(sess.ID) != hash || sess.Principal.Role != meta.role || sess.Principal.ID != meta.actorID {
		return nil, ErrSessionShadowIntegrity
	}
	now := time.Now()
	if !now.Before(sess.ExpiresAt) || !now.Before(meta.expires) ||
		(meta.role != "" && sess.Principal.FactorGeneration != meta.factor.String) {
		return nil, ErrSessionNotAuthorized
	}
	return &sess, nil
}

// Mirror copies the existing encrypted payload, not the raw cookie token.
// The key is the same SHA-256 cookie hash used by MySQL. NX and PXAT prevent
// overwrite and expiration extension; a retry verifies the prior value.
func (s SessionShadow) Mirror(ctx context.Context, hash string) error {
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size {
		return ErrSessionShadowIntegrity
	}
	var sealed string
	var meta sessionMeta
	err = s.DB.QueryRowContext(ctx, `SELECT w.payload,m.role,m.actor_id,m.expires_at,m.payload_digest,f.generation
		FROM web_session w JOIN shadow_session_authority m ON m.id_hash=w.id_hash
		LEFT JOIN auth_factor f ON f.role=m.role AND f.actor_id=m.actor_id AND f.enabled=true
		WHERE m.id_hash=? AND m.expires_at>UTC_TIMESTAMP(6)`, hash).
		Scan(&sealed, &meta.role, &meta.actorID, &meta.expires, &meta.digest, &meta.factor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionNotAuthorized
	}
	if err != nil {
		return err
	}
	if _, err = s.authenticate(hash, sealed, meta); err != nil {
		return err
	}
	key := sessionCacheKey(hash)
	err = s.Cache.Do(ctx, s.Cache.B().Set().Key(key).Value(sealed).Nx().Pxat(meta.expires).Build()).Error()
	if err == nil {
		return nil
	}
	// An uncertain SET or a pre-existing cache entry never authorizes a
	// session. Accept only identical ciphertext and the original expiry.
	prior, check := s.Cache.Do(ctx, s.Cache.B().Get().Key(key).Build()).ToString()
	if check != nil {
		return errors.Join(err, check)
	}
	if prior != sealed {
		return ErrSessionShadowIntegrity
	}
	expires, check := s.Cache.Do(ctx, s.Cache.B().Arbitrary("PEXPIRETIME").Keys(key).Build()).AsInt64()
	if check != nil {
		return check
	}
	if expires != meta.expires.UnixMilli() {
		return ErrSessionShadowIntegrity
	}
	return nil
}

// MirrorActive runs a bounded, restartable backfill. The SQL cursor closes
// before Valkey operations. Concurrent revocation cannot authorize stale
// values because Lookup still checks the live SQL metadata fence.
func (s SessionShadow) MirrorActive(ctx context.Context, maxRows int) (int, error) {
	if maxRows <= 0 {
		return 0, errors.New("positive session migration limit required")
	}
	var after string
	count := 0
	for {
		rows, err := s.DB.QueryContext(ctx, "SELECT id_hash FROM shadow_session_authority WHERE expires_at>UTC_TIMESTAMP(6) AND id_hash>? ORDER BY id_hash LIMIT 128", after)
		if err != nil {
			return count, err
		}
		var hashes []string
		for rows.Next() {
			var hash string
			if err = rows.Scan(&hash); err != nil {
				break
			}
			hashes = append(hashes, hash)
		}
		if err == nil {
			err = rows.Err()
		}
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return count, err
		}
		for _, hash := range hashes {
			if count == maxRows {
				return count, errors.New("session migration row limit reached")
			}
			if err = s.Mirror(ctx, hash); err != nil {
				if !errors.Is(err, ErrSessionNotAuthorized) {
					return count, err
				}
			} else {
				count++
			}
			after = hash
		}
		if len(hashes) < 128 {
			return count, nil
		}
	}
}

// Lookup must check the current MySQL authority after reading Valkey. Missing,
// expired, revoked, mismatched or unavailable state is never served from cache.
// This protects against restored stale values after a cache failover.
func (s SessionShadow) Lookup(ctx context.Context, id string) (*session.Session, error) {
	if id == "" {
		return nil, ErrSessionNotAuthorized
	}
	hash := sessionHash(id)
	sealed, err := s.Cache.Do(ctx, s.Cache.B().Get().Key(sessionCacheKey(hash)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return nil, ErrSessionNotAuthorized
	}
	if err != nil {
		return nil, err
	}
	var meta sessionMeta
	err = s.DB.QueryRowContext(ctx, `SELECT m.role,m.actor_id,m.expires_at,m.payload_digest,f.generation
		FROM shadow_session_authority m
		LEFT JOIN auth_factor f ON f.role=m.role AND f.actor_id=m.actor_id AND f.enabled=true
		WHERE m.id_hash=? AND m.expires_at>UTC_TIMESTAMP(6)`, hash).
		Scan(&meta.role, &meta.actorID, &meta.expires, &meta.digest, &meta.factor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotAuthorized
	}
	if err != nil {
		return nil, err
	}
	return s.authenticate(hash, sealed, meta)
}

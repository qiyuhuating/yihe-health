package lane

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func TestValkeySessionShadowRevocation(t *testing.T) {
	endpoint := os.Getenv("TEST_VALKEY_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_VALKEY_ENDPOINT not set")
	}
	password := os.Getenv("TEST_VALKEY_PASSWORD")
	if password == "" {
		t.Fatal("missing isolated Valkey password")
	}
	cache, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{endpoint}, Password: password, ForceSingleClient: true, DisableCache: true, DisableRetry: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cache.Close)
	pool := dbtest.MySQL(t)
	db := pool.DB
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	store.UseDatabase(db, box)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	create := func(p session.Principal) *session.Session {
		t.Helper()
		s := store.CreateContext(ctx, p)
		if s == nil {
			t.Fatal("real MySQL session creation failed")
		}
		return s
	}
	p := session.Principal{Role: "FAMILY", ID: 42, DisplayName: "synthetic"}
	old := create(p)
	// A quiesced fixture installs the projection before backfilling preexisting
	// rows. A production rollout needs a writer fence and reconciliation.
	for _, ddl := range []string{
		"CREATE TABLE shadow_session_authority (id_hash CHAR(64) PRIMARY KEY,role VARCHAR(16) NOT NULL,actor_id BIGINT NOT NULL,expires_at DATETIME(6) NOT NULL,payload_digest BINARY(32) NOT NULL,INDEX(role,actor_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin",
		"CREATE TRIGGER shadow_session_insert AFTER INSERT ON web_session FOR EACH ROW INSERT INTO shadow_session_authority VALUES(NEW.id_hash,NEW.role,NEW.actor_id,NEW.expires_at,UNHEX(SHA2(NEW.payload,256)))",
		`CREATE TRIGGER shadow_session_update AFTER UPDATE ON web_session FOR EACH ROW
			BEGIN
				DELETE FROM shadow_session_authority WHERE id_hash=OLD.id_hash;
				INSERT INTO shadow_session_authority VALUES(NEW.id_hash,NEW.role,NEW.actor_id,NEW.expires_at,UNHEX(SHA2(NEW.payload,256)));
			END`,
		"CREATE TRIGGER shadow_session_delete AFTER DELETE ON web_session FOR EACH ROW DELETE FROM shadow_session_authority WHERE id_hash=OLD.id_hash",
		"INSERT INTO shadow_session_authority SELECT id_hash,role,actor_id,expires_at,UNHEX(SHA2(payload,256)) FROM web_session",
	} {
		sinkExec(t, db, ddl)
	}
	second := create(p)
	third := create(session.Principal{Role: "ADMIN", ID: 77, FactorGeneration: "factor-one"})
	sinkExec(t, db, "INSERT INTO auth_factor(role,actor_id,secret,generation,enabled,pending_until,recovery_hashes) VALUES('ADMIN',77,'fixture','factor-one',true,UTC_TIMESTAMP(6),'[]')")
	shadow := SessionShadow{DB: db, Box: box, Cache: cache}
	if count, err := shadow.MirrorActive(ctx, 3); err != nil || count != 3 {
		t.Fatalf("bounded backfill: %d %v", count, err)
	}
	for _, sess := range []*session.Session{old, second, third} {
		hash := sessionHash(sess.ID)
		key := sessionCacheKey(hash)
		t.Cleanup(func() {
			c, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			_ = cache.Do(c, cache.B().Del().Key(key).Build()).Error()
		})
		if err = shadow.Mirror(ctx, hash); err != nil {
			t.Fatal(err)
		}
		if err = shadow.Mirror(ctx, hash); err != nil {
			t.Fatalf("idempotent mirror: %v", err)
		}
		got, err := shadow.Lookup(ctx, sess.ID)
		if err != nil || got.ID != sess.ID || got.CSRFToken != sess.CSRFToken || got.Principal != sess.Principal {
			t.Fatalf("session parity: %v", err)
		}
		cipher, err := cache.Do(ctx, cache.B().Get().Key(key).Build()).ToString()
		if err != nil || bytes.Contains([]byte(cipher), []byte(sess.ID)) || bytes.Contains([]byte(cipher), []byte(sess.CSRFToken)) {
			t.Fatal("plaintext session token exposed")
		}
	}
	reject := func(sess *session.Session) {
		t.Helper()
		got, err := shadow.Lookup(ctx, sess.ID)
		if got != nil || !errors.Is(err, ErrSessionNotAuthorized) {
			t.Fatalf("stale session authorized: %v", err)
		}
	}
	// SQL revocation transaction rollback must not revoke a still-live session.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM web_session WHERE id_hash=?", sessionHash(old.ID)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = shadow.Lookup(ctx, old.ID); err != nil {
		t.Fatalf("rolled-back revocation: %v", err)
	}

	// Delete using the existing production store. Valkey still contains stale
	// ciphertext, modelling an old replica or restored cache snapshot.
	if !store.DestroyContext(ctx, old.ID) {
		t.Fatal("destroy failed")
	}
	if _, err = cache.Do(ctx, cache.B().Get().Key(sessionCacheKey(sessionHash(old.ID))).Build()).ToString(); err != nil {
		t.Fatal("expected stale cache entry")
	}
	reject(old)
	if err = store.RevokeContext(ctx, p.Role, p.ID); err != nil {
		t.Fatal(err)
	}
	reject(second)
	if err = shadow.Mirror(ctx, sessionHash(second.ID)); !errors.Is(err, ErrSessionNotAuthorized) {
		t.Fatalf("revoked source mirrored: %v", err)
	}
	// Changing the factor generation without deleting the cached session also
	// rejects it, matching the existing database store's live factor check.
	sinkExec(t, db, "UPDATE auth_factor SET generation='factor-two' WHERE role='ADMIN' AND actor_id=77")
	reject(third)
	// Exercise integrity with a newly issued session, never by restoring an
	// old authentication generation.
	current := create(session.Principal{Role: "ADMIN", ID: 77, FactorGeneration: "factor-two"})
	key := sessionCacheKey(sessionHash(current.ID))
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = cache.Do(c, cache.B().Del().Key(key).Build()).Error()
	})
	if err = shadow.Mirror(ctx, sessionHash(current.ID)); err != nil {
		t.Fatal(err)
	}
	// A deliberately corrupted cached payload is rejected, not overwritten.
	if err = cache.Do(ctx, cache.B().Set().Key(key).Value("corrupted").Build()).Error(); err != nil {
		t.Fatal(err)
	}
	if got, err := shadow.Lookup(ctx, current.ID); got != nil || !errors.Is(err, ErrSessionShadowIntegrity) {
		t.Fatalf("corrupted session: %v", err)
	}
	if err = shadow.Mirror(ctx, sessionHash(current.ID)); !errors.Is(err, ErrSessionShadowIntegrity) {
		t.Fatalf("corrupt cache overwritten: %v", err)
	}
	if err = cache.Do(ctx, cache.B().Del().Key(key).Build()).Error(); err != nil {
		t.Fatal(err)
	}
	if err = shadow.Mirror(ctx, sessionHash(current.ID)); err != nil {
		t.Fatal(err)
	}
	sinkExec(t, db, "UPDATE web_session SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id_hash=?", sessionHash(current.ID))
	reject(current)

	// Database outage must not make an otherwise valid cached session usable.
	fresh := create(session.Principal{Role: "ADMIN", ID: 78})
	freshKey := sessionCacheKey(sessionHash(fresh.ID))
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = cache.Do(c, cache.B().Del().Key(freshKey).Build()).Error()
	})
	if err = shadow.Mirror(ctx, sessionHash(fresh.ID)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := shadow.Lookup(ctx, fresh.ID); got != nil || err == nil {
		t.Fatal("SQL outage accepted cached session")
	}
}

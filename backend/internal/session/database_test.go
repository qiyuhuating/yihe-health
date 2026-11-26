package session_test

import (
	"context"
	"encoding/base64"
	"strings"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
	"testing"
	"time"
)

func TestDatabaseSessionsShareRevokeAndFactorGeneration(t *testing.T) {
	pool := dbtest.MySQL(t)
	box, _ := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	a, b := session.NewStore(time.Hour), session.NewStore(time.Hour)
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	a.UseDatabase(pool.DB, box)
	b.UseDatabase(pool.DB, box)
	s := a.Create(session.Principal{Role: "ADMIN", ID: 1, Phone: "13800000001"})
	if s == nil {
		t.Fatal("create")
	}
	if got := b.Get(s.ID); got == nil || got.CSRFToken != s.CSRFToken {
		t.Fatal("replica cannot read session")
	}
	var payload, hash string
	if err := pool.QueryRowContext(context.Background(), "SELECT payload,id_hash FROM web_session LIMIT 1").Scan(&payload, &hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "13800000001") || strings.Contains(payload, s.CSRFToken) || hash == s.ID {
		t.Fatal("session secrets are plaintext")
	}
	if !a.Destroy(s.ID) || b.Get(s.ID) != nil {
		t.Fatal("cross-replica revocation failed")
	}
	old := a.Create(session.Principal{Role: "ADMIN", ID: 1})
	_, err := pool.ExecContext(context.Background(), "INSERT INTO auth_factor(role,actor_id,secret,generation,enabled,pending_until,recovery_hashes) VALUES('ADMIN',1,'encrypted','new-generation',true,CURRENT_TIMESTAMP(6),'[]')")
	if err != nil {
		t.Fatal(err)
	}
	if b.Get(old.ID) != nil {
		t.Fatal("session creation racing factor enable bypassed MFA")
	}
	current := a.Create(session.Principal{Role: "ADMIN", ID: 1, FactorGeneration: "new-generation"})
	if current == nil || b.Get(current.ID) == nil {
		t.Fatal("verified generation rejected")
	}
	if _, err = pool.ExecContext(context.Background(), "UPDATE auth_factor SET enabled=false WHERE role='ADMIN' AND actor_id=1"); err != nil {
		t.Fatal(err)
	}
	if b.Get(current.ID) != nil {
		t.Fatal("disabled factor retained the old generation")
	}
	withoutFactor := a.Create(session.Principal{Role: "ADMIN", ID: 1})
	if withoutFactor == nil || b.Get(withoutFactor.ID) == nil {
		t.Fatal("disabled factor rejected a session without generation")
	}
	if _, err = pool.ExecContext(context.Background(), "UPDATE auth_factor SET enabled=true,generation='rotated-generation' WHERE role='ADMIN' AND actor_id=1"); err != nil {
		t.Fatal(err)
	}
	if b.Get(current.ID) != nil || b.Get(withoutFactor.ID) != nil {
		t.Fatal("changed factor generation retained an old session")
	}
	current = a.Create(session.Principal{Role: "ADMIN", ID: 1, FactorGeneration: "rotated-generation"})
	if current == nil || b.Get(current.ID) == nil {
		t.Fatal("rotated generation rejected")
	}
	if err = a.Revoke("ADMIN", 1); err != nil || b.Get(current.ID) != nil {
		t.Fatal("principal revocation failed", err)
	}
	if _, err = pool.ExecContext(context.Background(), "DELETE FROM auth_factor WHERE actor_id=1"); err != nil {
		t.Fatal(err)
	}
	late := a.Create(session.Principal{Role: "ADMIN", ID: 1, FactorGeneration: "new-generation"})
	if late == nil || b.Get(late.ID) != nil {
		t.Fatal("session creation racing factor disable retained the old generation")
	}
	pool.Close()
	if a.Create(session.Principal{}) != nil || b.Get(old.ID) != nil || a.Destroy(old.ID) {
		t.Fatal("storage failure did not fail closed")
	}
}

func TestDatabaseSessionRejectsMismatchedIdentity(t *testing.T) {
	pool := dbtest.MySQL(t)
	box, _ := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	store.UseDatabase(pool.DB, box)
	for _, identity := range []struct {
		role string
		id   int64
	}{{"FAMILY", 1}, {"ADMIN", 2}} {
		sess := store.Create(session.Principal{Role: "ADMIN", ID: 1})
		if sess == nil {
			t.Fatal("create")
		}
		if _, err := pool.ExecContext(context.Background(), "UPDATE web_session SET role=?,actor_id=?", identity.role, identity.id); err != nil {
			t.Fatal(err)
		}
		if store.Get(sess.ID) != nil {
			t.Fatal("session accepted metadata different from encrypted principal")
		}
	}
	sess := store.Create(session.Principal{Role: "ADMIN", ID: 1})
	if sess == nil {
		t.Fatal("create")
	}
	if _, err := pool.ExecContext(context.Background(), "UPDATE web_session SET payload='corrupt'"); err != nil {
		t.Fatal(err)
	}
	if store.Get(sess.ID) != nil {
		t.Fatal("session accepted corrupt encrypted payload")
	}
}

package handlers

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

// Credential and account mutations must invalidate all prior sessions, not
// merely the one that initiated the request. The database-backed mode checks the same
// persistent session store used by different production replicas.
func TestFamilyCredentialMutationRevokesSessions(t *testing.T) {
	for _, mode := range []string{"memory", "database"} {
		t.Run(mode, func(t *testing.T) {
			pool := testDatabase(t)
			h := testHandler(t, pool)
			seedCareAdminDuty(t, pool, 11, "supervisor")
			var replica *session.Store
			if mode == "database" {
				box, err := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
				if err != nil {
					t.Fatal(err)
				}
				h.Sessions.UseDatabase(pool.DB, box)
				replica = session.NewStore(time.Hour)
				t.Cleanup(replica.Close)
				replica.UseDatabase(pool.DB, box)
			}
			const before = "BeforePassword123"
			const after = "ChangedPassword123"
			hash, err := auth.Hash(before)
			if err != nil {
				t.Fatal(err)
			}
			mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone,family_password) VALUES(1,'家属','13800000001',?)", hash)
			owner := session.Principal{Role: auth.RoleFamily, ID: 1, Phone: "13800000001"}
			first := h.Sessions.Create(owner)
			second := h.Sessions.Create(owner)
			other := h.Sessions.Create(session.Principal{Role: auth.RoleFamily, ID: 2, Phone: "13800000002"})
			if first == nil || second == nil || other == nil {
				t.Fatal("failed to seed sessions")
			}
			if replica != nil && replica.Get(first.ID) == nil {
				t.Fatal("replica did not see the existing session")
			}
			handler := func(w http.ResponseWriter, r *http.Request) {
				h.familyUpdatePassword(w, session.WithContext(r, first))
			}
			payload := map[string]any{
				"familyPhone": "13800000001", "oldPassword": before, "familyPassword": after,
			}
			out := api(t, handler, payload)
			if out["returnCode"] != "1111" {
				t.Fatalf("password change failed: %v", out)
			}
			var updated string
			if err := pool.QueryRowContext(context.Background(), "SELECT family_password FROM family WHERE family_id=1").Scan(&updated); err != nil {
				t.Fatal(err)
			}
			if !auth.Matches(after, updated) || auth.Matches(before, updated) {
				t.Fatal("password did not rotate")
			}
			for _, sess := range []*session.Session{first, second} {
				if h.Sessions.Get(sess.ID) != nil {
					t.Fatalf("old family session survived password change: %s", sess.ID)
				}
				if replica != nil && replica.Get(sess.ID) != nil {
					t.Fatalf("old family session survived on replica: %s", sess.ID)
				}
			}
			if h.Sessions.Get(other.ID) == nil {
				t.Fatal("another account's session was revoked")
			}
			if mode == "database" {
				var stale int
				if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM web_session WHERE role='FAMILY' AND actor_id=1").Scan(&stale); err != nil || stale != 0 {
					t.Fatalf("persisted old sessions=%d err=%v", stale, err)
				}
			}
			newSession := h.Sessions.Create(owner)
			if newSession == nil {
				t.Fatal("could not create new session after changing password")
			}
			deleted := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.familyDelete), map[string]any{"familyId": 1})
			if deleted["returnCode"] != "1111" {
				t.Fatalf("family removal failed: %v", deleted)
			}
			if h.Sessions.Get(newSession.ID) != nil {
				t.Fatal("deleted family's session survived")
			}
			if replica != nil && replica.Get(newSession.ID) != nil {
				t.Fatal("deleted family's session survived on replica")
			}
			if h.Sessions.Get(other.ID) == nil {
				t.Fatal("other family session was affected by deletion")
			}
		})
	}
}

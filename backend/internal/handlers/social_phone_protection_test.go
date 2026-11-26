package handlers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

// This regression exercises the actual comment/like handlers and durable MySQL
// rows. Masking a response alone does not protect a stored phone copy.
func TestSocialPhonesAreProtectedMySQL(t *testing.T) {
	store := testDatabase(t)
	h := testHandler(t, store)
	key := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 32))) }
	var err error
	h.Box, err = securestore.NewKeyring("social-test", map[string]string{"social-test": key("A")}, key("B"))
	if err != nil {
		t.Fatal(err)
	}
	const phone = "13800000001"
	mustExec(t, store, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'家属',?)", phone)
	mustExec(t, store, "INSERT INTO `text`(text_id,title,text_main,time) VALUES(1,'公告','测试',CURRENT_TIMESTAMP(6))")
	viewer := session.Principal{Role: auth.RoleFamily, ID: 1, Phone: phone, DisplayName: "家属"}
	create := api(t, withHealthPrincipal(h, viewer, h.createNoticeComment), map[string]any{"textId": "1", "said": "留言"})
	if create["returnCode"] != "1111" {
		t.Fatal("comment creation did not succeed")
	}
	ctx := context.Background()
	var id int64
	var raw sql.NullString
	if err := store.QueryRowContext(ctx, "SELECT said_id,said_phone FROM said WHERE text_id=1").Scan(&id, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Valid {
		t.Error("comment author phone remains in the legacy plaintext column with encryption enabled")
	}
	checkPublic := func(liked string) {
		t.Helper()
		out := api(t, withHealthPrincipal(h, viewer, h.listNoticeComments), map[string]any{"textId": "1"})
		items, ok := out["comments"].([]any)
		if !ok || len(items) != 1 {
			t.Fatal("comment list changed")
		}
		item := items[0].(map[string]any)
		if item["saidPhone"] != "138****0001" || item["good"] != liked {
			t.Fatal("public masked phone or viewer vote status changed")
		}
	}
	checkPublic("0")
	api(t, withHealthPrincipal(h, viewer, h.toggleNoticeCommentLike), map[string]any{"saidId": id})
	if err := store.QueryRowContext(ctx, "SELECT phone FROM nice_count WHERE said_id=?", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw.Valid {
		t.Error("like identity remains in the legacy plaintext phone column with encryption enabled")
	}
	checkPublic("1")
	api(t, withHealthPrincipal(h, viewer, h.toggleNoticeCommentLike), map[string]any{"saidId": id})
	checkPublic("0")
	var votes, count int64
	if err := store.QueryRowContext(ctx, "SELECT COUNT(*) FROM nice_count WHERE said_id=?", id).Scan(&votes); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRowContext(ctx, "SELECT nice_count FROM said WHERE said_id=?", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if votes != 0 || count != 0 {
		t.Fatal("toggle must remove the same viewer's vote and update the aggregate atomically")
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func TestProtectNotesRejectsUnboundedOrUnknownOptionsBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{
		{"--limit", "0"}, {"--limit", "1001"}, {"--limit", "wrong"},
		{"--unknown"}, {"extra"},
	} {
		var out bytes.Buffer
		if err := run(args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("invalid args %q produced output or were accepted: %v", args, err)
		}
	}
	var help bytes.Buffer
	if err := run([]string{"--help"}, &help); err != nil || !strings.Contains(help.String(), "1..1000") {
		t.Fatalf("help requires a database or omitted the bound: %v", err)
	}
}

func TestBoundedNoteConversionMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"INSERT INTO oldperson(old_id,name) VALUES(1,'Test')",
		"INSERT INTO device(device_id,device_no,secret) VALUES(1,'NOTE-TEST','secret')",
		"INSERT INTO care_incident(id,old_id,device_id,resolution_note) VALUES(1,1,1,'note-1'),(2,1,1,'note-2'),(3,1,1,'note-3')",
		"INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(1,'test','review','event-note')",
		"INSERT INTO alert_review(entity,record_id,kind,verdict,reason,actor_id) VALUES('care',1,'sos','confirmed','review-note',1)",
	} {
		if _, err := pool.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := convertNotes(ctx, pool.DB, box, 1, true, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "capped=true") {
		t.Fatal("partial backfill was not marked as capped")
	}
	var protected int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident WHERE resolution_note LIKE 'sne1:%'").Scan(&protected); err != nil || protected != 1 {
		t.Fatalf("limit was ignored: %d %v", protected, err)
	}
	out.Reset()
	if err := convertNotes(ctx, pool.DB, box, 100, true, false, &out); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"SELECT COUNT(*) FROM care_incident WHERE resolution_note LIKE 'sne1:%'", 3},
		{"SELECT COUNT(*) FROM care_incident_event WHERE note LIKE 'sne1:%'", 1},
		{"SELECT COUNT(*) FROM alert_review WHERE reason LIKE 'sne1:%'", 1},
	} {
		if err := pool.QueryRowContext(ctx, tc.query).Scan(&protected); err != nil || protected != tc.want {
			t.Fatalf("incomplete protected note rows: %q count=%d err=%v", tc.query, protected, err)
		}
	}
	out.Reset()
	if err := convertNotes(ctx, pool.DB, box, 100, true, false, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "applied=1") {
		t.Fatalf("idempotent run rewrote rows: %s", out.String())
	}
	out.Reset()
	if err := convertNotes(ctx, pool.DB, box, 100, true, true, &out); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ query, want string }{
		{"SELECT resolution_note FROM care_incident WHERE id=1", "note-1"},
		{"SELECT note FROM care_incident_event WHERE incident_id=1", "event-note"},
		{"SELECT reason FROM alert_review WHERE record_id=1", "review-note"},
	} {
		var text string
		if err := pool.QueryRowContext(ctx, tc.query).Scan(&text); err != nil || text != tc.want {
			t.Fatalf("decrypt did not preserve content: %q value=%q err=%v", tc.query, text, err)
		}
	}
}

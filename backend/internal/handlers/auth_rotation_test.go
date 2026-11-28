package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestDatabaseLoginRotationAtomic(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 41, "supervisor")
	a, b := testHandler(t, pool), testHandler(t, pool)
	a.Sessions.UseDatabase(pool.DB, a.Box)
	b.Sessions.UseDatabase(pool.DB, b.Box)
	principal := session.Principal{Role: auth.RoleAdmin, ID: 41, Phone: "13800000041"}
	old := a.Sessions.Create(session.Principal{})
	if old == nil {
		t.Fatal("anonymous session")
	}
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: old.ID})
		return r
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := pool.QueryRowContext(t.Context(), q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	mustExec(t, pool, "CREATE TRIGGER fail_rotation_audit BEFORE INSERT ON login_audit FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected audit failure'")
	w := httptest.NewRecorder()
	if a.renewSession(w, request(), principal) || w.Code != 500 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("failed audit issued success: %d %s", w.Code, w.Body.String())
	}
	if b.Sessions.Get(old.ID) == nil || count("SELECT COUNT(*) FROM web_session") != 1 || count("SELECT COUNT(*) FROM login_audit") != 0 {
		t.Fatal("failed audit did not roll back rotation")
	}
	mustExec(t, pool, "DROP TRIGGER fail_rotation_audit")
	// Two independent stores simulate replicas after credentials have
	// already been verified. Exactly one may consume the old cookie.
	var wg sync.WaitGroup
	start := make(chan struct{})
	type outcome struct {
		ok bool
		w  *httptest.ResponseRecorder
	}
	result := make(chan outcome, 2)
	for _, h := range []*Handler{a, b} {
		wg.Go(func() {
			<-start
			w := httptest.NewRecorder()
			ok := h.renewSession(w, request(), principal)
			result <- outcome{ok, w}
		})
	}
	close(start)
	wg.Wait()
	close(result)
	success := 0
	for r := range result {
		if r.ok {
			success++
			cookies := r.w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Value == old.ID {
				t.Fatal("session fixation")
			}
			current := b.Sessions.Get(cookies[0].Value)
			if current == nil || current.Principal != principal || current.CSRFToken == old.CSRFToken {
				t.Fatal("rotated session unavailable on peer")
			}
		} else if r.w.Code != 403 || len(r.w.Result().Cookies()) != 0 {
			t.Fatalf("stale cookie accepted: %d %s", r.w.Code, r.w.Body.String())
		}
	}
	if success != 1 || a.Sessions.Get(old.ID) != nil || b.Sessions.Get(old.ID) != nil || count("SELECT COUNT(*) FROM web_session") != 1 || count("SELECT COUNT(*) FROM login_audit WHERE outcome='success'") != 1 {
		t.Fatal("rotation was not exactly once")
	}
}

type binlogPosition struct {
	file string
	pos  uint64
}

func readRotationBinlogPosition(t *testing.T, db *sql.DB) binlogPosition {
	t.Helper()
	var p binlogPosition
	var include, exclude, gtid string
	if err := db.QueryRowContext(t.Context(), "SHOW BINARY LOG STATUS").Scan(&p.file, &p.pos, &include, &exclude, &gtid); err != nil {
		t.Fatal(err)
	}
	return p
}

func rotationXIDs(t *testing.T, db *sql.DB, before, after binlogPosition) int {
	t.Helper()
	if before.file != after.file {
		t.Fatal("binlog rotated during isolated interval")
	}
	rows, err := db.QueryContext(t.Context(), "SHOW BINLOG EVENTS IN ? FROM ?", before.file, before.pos)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var file, kind, info string
		var pos, server, end uint64
		if err = rows.Scan(&file, &pos, &kind, &server, &end, &info); err != nil {
			t.Fatal(err)
		}
		if end > after.pos {
			break
		}
		if kind == "Xid" {
			n++
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return n
}

// The comparison deliberately excludes password verification and fixture
// creation. It measures only the real successful session/audit writes.
// Use exclusively with a disposable binlog-enabled MySQL server.
func TestSessionRotationCommitComparison(t *testing.T) {
	if os.Getenv("YIHE_ROTATION_AB") != "1" {
		t.Skip("opt-in: set YIHE_ROTATION_AB=1 for isolated session rotation comparison")
	}
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 41, "supervisor")
	h := testHandler(t, pool)
	h.Sessions.UseDatabase(pool.DB, h.Box)
	p := session.Principal{Role: auth.RoleAdmin, ID: 41, Phone: "13800000041"}
	const n = 64
	for _, atomic := range []bool{false, true, true, false} {
		old := make([]*session.Session, n)
		for i := range old {
			old[i] = h.Sessions.Create(session.Principal{})
			if old[i] == nil {
				t.Fatal("fixture")
			}
		}
		before := readRotationBinlogPosition(t, pool.DB)
		times := make([]time.Duration, n)
		start := time.Now()
		for i, s := range old {
			r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: s.ID})
			begin := time.Now()
			if atomic {
				if !h.renewSession(httptest.NewRecorder(), r, p) {
					t.Fatal("atomic login")
				}
			} else {
				if !h.Sessions.DestroyContext(context.Background(), s.ID) || h.Sessions.CreateContext(context.Background(), p) == nil || h.loginAudit(r, p.Phone, p.Role, p.ID, "success") != nil {
					t.Fatal("legacy login")
				}
			}
			times[i] = time.Since(begin)
		}
		elapsed := time.Since(start)
		after := readRotationBinlogPosition(t, pool.DB)
		count := rotationXIDs(t, pool.DB, before, after)
		want := n * 3
		if atomic {
			want = n
		}
		if count != want {
			t.Fatalf("atomic=%v Xid events=%d want=%d", atomic, count, want)
		}
		slices.Sort(times)
		t.Logf("ROTATION atomic=%v n=%d xid=%d binlog_bytes=%d rps=%.2f p50_ms=%.6f p99_ms=%.6f", atomic, n, count, after.pos-before.pos, float64(n)/elapsed.Seconds(), float64(times[n/2])/float64(time.Millisecond), float64(times[(n*99+99)/100-1])/float64(time.Millisecond))
	}
}

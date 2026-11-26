package notify

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func TestBatchEnqueueBoundariesIdempotenceAndRollback(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := context.Background()
	box, err := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]Message, 601)
	for i := range messages {
		messages[i] = Message{Topic: "announcement", Key: strconv.Itoa(i), FamilyID: 1, Phone: "13800000001"}
	}
	for _, commit := range []bool{false, true, true} {
		tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		if err = EnqueueBatch(ctx, tx, box, messages); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox").Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if commit {
			want = 1202
		}
		if count != want {
			t.Fatalf("commit=%v: %d rows, want %d", commit, count, want)
		}
	}
	var encrypted string
	if err = p.QueryRowContext(ctx, "SELECT recipient FROM message_outbox LIMIT 1").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, securestore.Prefix) {
		t.Fatal("batch recipient was not encrypted")
	}
	if plain, err := box.Open(encrypted, "notification-recipient"); err != nil || plain != "13800000001" {
		t.Fatal(plain, err)
	}
	var available int
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE channel='in_app' AND status='delivered' AND delivered_at IS NOT NULL AND attempts=0").Scan(&available); err != nil || available != 601 {
		t.Fatal("committed local inbox entries were unnecessarily queued", available, err)
	}
}

func TestBatchExternalConcurrencyBoundAndAcceptedStatus(t *testing.T) {
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	p := dbtest.MySQL(t)
	ctx := context.Background()
	messages := make([]Message, 65)
	for i := range messages {
		messages[i] = Message{Topic: "announcement", Key: strconv.Itoa(i), FamilyID: 1, Phone: "13800000001", Channels: []string{"sms"}}
	}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = EnqueueBatch(ctx, tx, nil, messages); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	arrived, release := make(chan struct{}, 65), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" || !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "message-") {
			t.Error("missing gateway authentication or stable idempotency key")
		}
		arrived <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	defer unblock()
	client := server.Client()
	client.Timeout = 10 * time.Second
	d := Worker{DB: p, URL: server.URL, Token: "test", Client: client}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		n, err := d.Drain(ctx, 512)
		if err == nil && n != 32 {
			t.Errorf("claimed %d external jobs, want 32", n)
		}
		done <- err
	}()
	for range 32 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("external requests serialized or failed to arrive")
		}
	}
	var pending, sending int
	if err = p.QueryRowContext(ctx, "SELECT sum(status='pending'),sum(status='sending') FROM message_outbox").Scan(&pending, &sending); err != nil || pending != 33 || sending != 32 {
		t.Fatal("claim exceeded external bound", pending, sending, err)
	}
	unblock()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var sent, delivered int
	if err = p.QueryRowContext(ctx, "SELECT sum(status='sent'),sum(status='delivered') FROM message_outbox").Scan(&sent, &delivered); err != nil || sent != 32 || delivered != 0 {
		t.Fatal("provider acceptance became delivery", sent, delivered, err)
	}
}

func TestBatchDrainLocalJobsBeyondSingleStatement(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := context.Background()
	messages := make([]Message, 1100)
	for i := range messages {
		messages[i] = Message{Topic: "announcement", Key: strconv.Itoa(i), FamilyID: 1, Channels: []string{"in_app"}}
	}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = EnqueueBatch(ctx, tx, nil, messages); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Explicitly construct legacy jobs: new local entries need no worker.
	if _, err = p.ExecContext(ctx, "UPDATE message_outbox SET status='pending',delivered_at=NULL"); err != nil {
		t.Fatal(err)
	}
	d := Worker{DB: p}
	for _, want := range []int{512, 512, 76, 0} {
		if n, err := d.Drain(ctx, 10000); err != nil || n != want {
			t.Fatal(n, want, err)
		}
	}
	var delivered int
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE status='delivered' AND attempts=1 AND delivered_at IS NOT NULL").Scan(&delivered); err != nil || delivered != 1100 {
		t.Fatal(delivered, err)
	}
}

func TestStaleAndDueRetryAreNotStarvedByFreshPendingJobs(t *testing.T) {
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	p := dbtest.MySQL(t)
	ctx := context.Background()
	// A fresh pending stream has older retry_at values than retry/reclaimed
	// tasks. Status priority must still recover the stale lease and due retry.
	for _, state := range []string{"pending", "failed", "sending"} {
		if _, err := p.ExecContext(ctx, `INSERT INTO message_outbox(topic,source_key,family_id,channel,recipient,status,retry_at,last_attempt_at)
 VALUES('announcement',?,1,'sms','13800000001',?,?,?)`, state, state, time.Now().Add(-time.Minute), time.Now().Add(-2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	order := make(chan string, 3)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Source string `json:"sourceId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		order <- request.Source
		w.WriteHeader(http.StatusAccepted)
	}))
	defer gateway.Close()
	d := Worker{DB: p, URL: gateway.URL, Token: "test", Client: gateway.Client()}
	for _, want := range []string{"sending", "failed", "pending"} {
		if n, err := d.Drain(ctx, 1); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		if got := <-order; got != want {
			t.Fatalf("claimed %s before %s", got, want)
		}
	}
}

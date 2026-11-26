package notify

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func TestBulkRequiresUnambiguousPerNotificationAcceptance(t *testing.T) {
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	t.Setenv("TENCENT_SMS_APPID", "")
	box, err := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("13800000001", "notification-recipient")
	if err != nil {
		t.Fatal(err)
	}
	jobs := []messageJob{
		{id: 10, topic: "announcement", key: "one", channel: "sms", recipient: sealed},
		{id: 20, topic: "announcement", key: "one", channel: "voice", recipient: sealed},
	}
	for _, tc := range []struct {
		name, body string
		accepted   []bool
	}{
		{"all", `{"results":[{"notificationId":20,"accepted":true},{"notificationId":10,"accepted":true}]}`, []bool{true, true}},
		{"missing", `{"results":[{"notificationId":10,"accepted":true}]}`, []bool{true, false}},
		{"false", `{"results":[{"notificationId":10,"accepted":false},{"notificationId":20,"accepted":true}]}`, []bool{false, true}},
		{"empty", `{}`, []bool{false, false}},
		{"HTTP status alone", ``, []bool{false, false}},
		{"unknown", `{"results":[{"notificationId":10,"accepted":true},{"notificationId":99,"accepted":true}]}`, []bool{false, false}},
		{"duplicate", `{"results":[{"notificationId":10,"accepted":true},{"notificationId":10,"accepted":false}]}`, []bool{false, false}},
		{"trailing JSON", `{"results":[{"notificationId":10,"accepted":true}]} {}`, []bool{false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test" {
					t.Error("missing authentication")
				}
				var request struct {
					Notifications []bulkNotification `json:"notifications"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Notifications) != 2 {
					t.Error("invalid request", err)
				}
				for _, item := range request.Notifications {
					if item.Recipient != "13800000001" || item.IdempotencyKey != fmt.Sprintf("message-%d", item.NotificationID) {
						t.Error("recipient decryption or stable item key was lost")
					}
				}
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprint(w, tc.body)
			}))
			defer gateway.Close()
			d := Worker{Box: box, BulkURL: gateway.URL, Token: "test", Client: gateway.Client()}
			results := d.deliverBulk(context.Background(), jobs)
			for i, accepted := range tc.accepted {
				if (results[i] == nil) != accepted {
					t.Errorf("notification %d accepted=%v want=%v", jobs[i].id, results[i] == nil, accepted)
				}
			}
		})
	}
	t.Setenv("TENCENT_SMS_APPID", "configured")
	if (&Worker{BulkURL: "https://bulk.example"}).bulkEnabled() {
		t.Fatal("bulk silently replaced Tencent's configured adapter")
	}
}

func TestBulkStaleLeaseCannotOverwriteAcceptanceOrCreateFallback(t *testing.T) {
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	t.Setenv("TENCENT_SMS_APPID", "")
	p := dbtest.MySQL(t)
	ctx := context.Background()
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	messages := []Message{
		{Topic: "announcement", Key: "one", FamilyID: 1, Phone: "13800000001", Channels: []string{"sms"}},
		{Topic: "announcement", Key: "two", FamilyID: 1, Phone: "13800000001", Channels: []string{"sms"}},
	}
	if err = EnqueueBatch(ctx, tx, nil, messages); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE message_outbox SET attempts=4"); err != nil {
		t.Fatal(err)
	}
	arrived, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(arrived)
			<-release
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var request struct {
			Notifications []bulkNotification `json:"notifications"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		results := make([]map[string]any, 0, len(request.Notifications))
		for _, item := range request.Notifications {
			results = append(results, map[string]any{"notificationId": item.NotificationID, "accepted": true})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer gateway.Close()
	d := Worker{DB: p, BulkURL: gateway.URL, Token: "test", Client: gateway.Client()}
	done := make(chan error, 1)
	go func() { _, err := d.Drain(ctx, 512); done <- err }()
	<-arrived
	if _, err = p.ExecContext(ctx, "UPDATE message_outbox SET last_attempt_at=?", time.Now().Add(-2*time.Minute)); err != nil {
		close(release)
		t.Fatal(err)
	}
	if n, err := d.Drain(ctx, 512); err != nil || n != 2 {
		close(release)
		t.Fatal(n, err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var sent, delivered, fallback int
	if err = p.QueryRowContext(ctx, "SELECT sum(status='sent'),sum(status='delivered'),sum(channel='voice') FROM message_outbox").Scan(&sent, &delivered, &fallback); err != nil || sent != 2 || delivered != 0 || fallback != 0 {
		t.Fatal(sent, delivered, fallback, err)
	}
}

func TestBulkBoundPartialAcceptanceAndFifthFailureFallback(t *testing.T) {
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	t.Setenv("TENCENT_SMS_APPID", "")
	p := dbtest.MySQL(t)
	ctx := context.Background()
	box, err := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]Message, 513)
	for i := range messages {
		messages[i] = Message{Topic: "announcement", Key: fmt.Sprint(i), FamilyID: 1, Phone: "13800000001", Channels: []string{"sms"}}
	}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = EnqueueBatch(ctx, tx, box, messages); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE message_outbox SET attempts=4"); err != nil {
		t.Fatal(err)
	}
	var received atomic.Int64
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Notifications []bulkNotification `json:"notifications"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Notifications) != 512 {
			t.Error("bulk bound", len(request.Notifications), err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received.Add(int64(len(request.Notifications)))
		// Explicit rejection and missing entries both remain failed. Only the
		// first item has a provider acceptance; HTTP 202 adds no other success.
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"results":[{"notificationId":%d,"accepted":true},{"notificationId":%d,"accepted":false}]}`, request.Notifications[0].NotificationID, request.Notifications[1].NotificationID)
	}))
	defer gateway.Close()
	d := Worker{DB: p, Box: box, BulkURL: gateway.URL, Token: "test", Client: gateway.Client()}
	if n, err := d.Drain(ctx, 10000); err != nil || n != 512 || received.Load() != 512 {
		t.Fatal(n, received.Load(), err)
	}
	var sent, exhausted, delivered, encryptedFallback, unclaimed int
	if err = p.QueryRowContext(ctx, `SELECT sum(status='sent'),sum(status='exhausted'),sum(status='delivered'),
 sum(channel='voice' AND recipient LIKE 'sne1:%'),sum(channel='sms' AND status='pending' AND attempts=4) FROM message_outbox`).Scan(&sent, &exhausted, &delivered, &encryptedFallback, &unclaimed); err != nil || sent != 1 || exhausted != 511 || delivered != 0 || encryptedFallback != 511 || unclaimed != 1 {
		t.Fatal(sent, exhausted, delivered, encryptedFallback, unclaimed, err)
	}
}

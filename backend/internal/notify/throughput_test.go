package notify

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

// This opt-in measurement includes real MySQL commits, AES-GCM recipients and
// lease completion writes. The HTTP case is simulated provider acceptance only.
func TestNotificationThroughput(t *testing.T) {
	if os.Getenv("NOTIFY_PERF") != "1" {
		t.Skip("set NOTIFY_PERF=1 for isolated database measurements")
	}
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	for _, mode := range []string{"in_app", "sms", "bulk_sms"} {
		t.Run(mode, func(t *testing.T) {
			channel := mode
			if mode == "bulk_sms" {
				channel = "sms"
			}
			p := dbtest.MySQL(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			box, err := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
			if err != nil {
				t.Fatal(err)
			}
			const count, batch, replicas = 16384, 512, 8
			var next atomic.Int64
			errors := make(chan error, replicas)
			var writers sync.WaitGroup
			started := time.Now()
			for range replicas {
				writers.Add(1)
				go func() {
					defer writers.Done()
					for {
						first := int(next.Add(batch)) - batch
						if first >= count {
							return
						}
						messages := make([]Message, min(batch, count-first))
						for i := range messages {
							messages[i] = Message{Topic: "perf", Key: fmt.Sprint(first + i), FamilyID: 1, Phone: "13800000001", Channels: []string{channel}}
						}
						tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
						if err == nil {
							err = EnqueueBatch(ctx, tx, box, messages)
							if err == nil {
								err = tx.Commit()
							}
							tx.Rollback()
						}
						if err != nil {
							errors <- err
							return
						}
					}
				}()
			}
			writers.Wait()
			enqueueElapsed := time.Since(started)
			select {
			case err = <-errors:
				t.Fatal(err)
			default:
			}
			var persisted int
			if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox").Scan(&persisted); err != nil || persisted != count {
				t.Fatal(persisted, err)
			}
			if channel == "in_app" {
				var available int
				if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE status='delivered' AND delivered_at IS NOT NULL AND attempts=0").Scan(&available); err != nil || available != count {
					t.Fatal(available, err)
				}
				t.Logf("actual committed locally available inbox rows=%d writers=%d enqueue=%s %.0f rows/s; no worker/network/reader acknowledgement involved", persisted, replicas, enqueueElapsed, count/enqueueElapsed.Seconds())
				return
			}
			var plan string
			if err = p.QueryRowContext(ctx, `EXPLAIN FORMAT=JSON SELECT id,family_id,topic,source_key,channel,recipient,attempts FROM message_outbox
 WHERE ((status IN ('pending','failed') AND retry_at<=CURRENT_TIMESTAMP(6)) OR (status='sending' AND last_attempt_at<?))
 ORDER BY retry_at,id LIMIT 512 FOR UPDATE SKIP LOCKED`, time.Now().Add(-time.Minute)).Scan(&plan); err != nil {
				t.Fatal(err)
			}
			t.Logf("current OR selector query plan: %s", plan)
			if err = p.QueryRowContext(ctx, `EXPLAIN FORMAT=JSON SELECT id,family_id,topic,source_key,channel,recipient,attempts FROM message_outbox
 WHERE status='pending' AND retry_at<=? ORDER BY retry_at,id LIMIT 512 FOR UPDATE SKIP LOCKED`, time.Now()).Scan(&plan); err != nil {
				t.Fatal(err)
			}
			t.Logf("status equality selector query plan: %s", plan)
			var accepted atomic.Int64
			var requests atomic.Int64
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer measurement" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				requests.Add(1)
				if mode == "bulk_sms" {
					var request struct {
						Notifications []bulkNotification `json:"notifications"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					results := make([]map[string]any, 0, len(request.Notifications))
					for _, item := range request.Notifications {
						if item.IdempotencyKey != fmt.Sprintf("message-%d", item.NotificationID) || item.Recipient != "13800000001" {
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						results = append(results, map[string]any{"notificationId": item.NotificationID, "accepted": true})
					}
					accepted.Add(int64(len(results)))
					w.WriteHeader(http.StatusAccepted)
					json.NewEncoder(w).Encode(map[string]any{"results": results})
					return
				}
				if r.Header.Get("Idempotency-Key") == "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				accepted.Add(1)
				w.WriteHeader(http.StatusAccepted)
			}))
			defer gateway.Close()
			client := gateway.Client()
			client.Timeout = 10 * time.Second
			transport := client.Transport.(*http.Transport)
			transport.MaxIdleConns = replicas * 32
			transport.MaxIdleConnsPerHost = replicas * 32
			transport.MaxConnsPerHost = replicas * 32
			d := Worker{DB: p, Box: box, URL: gateway.URL, Token: "measurement", Client: client}
			if mode == "bulk_sms" {
				d.BulkURL = gateway.URL
			}
			var processed atomic.Int64
			started = time.Now()
			for range replicas {
				writers.Add(1)
				go func() {
					defer writers.Done()
					for {
						n, err := d.Drain(ctx, batch)
						processed.Add(int64(n))
						if err != nil {
							errors <- err
							return
						}
						if n == 0 {
							return
						}
					}
				}()
			}
			writers.Wait()
			drainElapsed := time.Since(started)
			select {
			case err = <-errors:
				t.Fatal(err)
			default:
			}
			state := "delivered"
			if channel == "sms" {
				state = "sent"
				if accepted.Load() != count {
					var failed, pending, sending int
					p.QueryRowContext(ctx, "SELECT sum(status='failed'),sum(status='pending'),sum(status='sending') FROM message_outbox").Scan(&failed, &pending, &sending)
					t.Fatal("gateway acceptance count", accepted.Load(), "failed", failed, "pending", pending, "sending", sending)
				}
			}
			var finished int
			if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE status=?", state).Scan(&finished); err != nil || finished != count || processed.Load() != count {
				t.Fatal(finished, processed.Load(), err)
			}
			t.Logf("actual committed rows=%d writers=%d enqueue=%s %.0f rows/s; actual workers=%d drain=%s %.0f rows/s final=%s simulated_provider_ACKs=%d HTTP_requests=%d enqueue+drain=%.0f rows/s", persisted, replicas, enqueueElapsed, count/enqueueElapsed.Seconds(), replicas, drainElapsed, count/drainElapsed.Seconds(), state, accepted.Load(), requests.Load(), count/(enqueueElapsed+drainElapsed).Seconds())
		})
	}
}

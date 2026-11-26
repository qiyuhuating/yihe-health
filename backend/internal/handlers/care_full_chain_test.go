package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/care"
	"yihe-care/internal/config"
	"yihe-care/internal/csrf"
	"yihe-care/internal/session"
)

// A real TLS gateway completes processing after the dispatcher's 12s timeout.
// This test never edits outbox status/attempts to manufacture a successful path.
func TestCareFullHTTPChainAfterRealGatewayTimeout(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'synthetic','13800000001')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(1,'resolver','1'),(2,'supervisor','1')")
	mustExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(1,'caregiver',2),(2,'supervisor',2)")
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	h := New(pool, store, config.Config{DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))})
	secret := "synthetic-receipt-secret-at-least-32-characters"
	t.Setenv("CARE_RECEIPT_SECRET", secret)
	receipt := httptest.NewServer(http.HandlerFunc(h.careReceipt))
	defer receipt.Close()
	arrived, release, completed := make(chan int64, 1), make(chan struct{}), make(chan error, 1)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var job struct {
			NotificationID int64 `json:"notificationId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			completed <- err
			return
		}
		if r.Header.Get("Idempotency-Key") != fmt.Sprintf("care-outbox-%d", job.NotificationID) {
			completed <- fmt.Errorf("missing stable idempotency key")
			return
		}
		arrived <- job.NotificationID
		<-release
		body, _ := json.Marshal(map[string]any{"notificationId": job.NotificationID, "status": "delivered"})
		for range 2 {
			stamp := strconv.FormatInt(time.Now().Unix(), 10)
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(stamp + "."))
			mac.Write(body)
			req, _ := http.NewRequest(http.MethodPost, receipt.URL, bytes.NewReader(body))
			req.Header.Set("X-Care-Timestamp", stamp)
			req.Header.Set("X-Care-Signature", hex.EncodeToString(mac.Sum(nil)))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				completed <- err
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				completed <- fmt.Errorf("receipt HTTP %d", resp.StatusCode)
				return
			}
		}
		completed <- nil
		w.WriteHeader(http.StatusAccepted)
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); gateway.Close() }()
	// The production gateway client builds its own TLS transport; provide the test CA explicitly.
	caPath := filepath.Join(t.TempDir(), "gateway-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: gateway.Certificate().Raw})
	if err := os.WriteFile(caPath, caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CARE_NOTIFY_CA_FILE", caPath)
	t.Setenv("CARE_NOTIFY_WEBHOOK_URL", gateway.URL)
	t.Setenv("CARE_NOTIFY_WEBHOOK_TOKEN", "synthetic-gateway-token")
	press := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "full-chain-real-timeout", "bindingVersion": 1}
	first := api(t, h.deviceSos, press)
	duplicate := api(t, h.deviceSos, press)
	if first["incidentId"] != duplicate["incidentId"] || duplicate["duplicate"] != true {
		t.Fatal("duplicate SOS event")
	}
	id := int64(first["incidentId"].(float64))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); care.Run(ctx, pool, h.Box) }()
	defer func() { cancel(); <-done }()
	var notificationID int64
	select {
	case notificationID = <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("gateway did not receive durable outbox job")
	}
	deadline := time.Now().Add(20 * time.Second)
	var status string
	for {
		if err := pool.QueryRowContext(ctx, "SELECT status FROM care_notification_outbox WHERE id=?", notificationID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual request timeout did not persist failure: %s", status)
		}
		time.Sleep(25 * time.Millisecond)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late receipt did not finish")
	}
	var state string
	var acknowledged *string
	if err := pool.QueryRowContext(ctx, "SELECT i.state,i.acknowledged_by,o.status FROM care_incident i JOIN care_notification_outbox o ON o.incident_id=i.id WHERE i.id=?", id).Scan(&state, &acknowledged, &status); err != nil {
		t.Fatal(err)
	}
	if state != "notified" || acknowledged != nil || status != "delivered" {
		t.Fatalf("delivery falsely implies human action: %s %v %s", state, acknowledged, status)
	}
	transition := csrf.Middleware(store, auth.Middleware(store, http.HandlerFunc(h.careTransition)))
	act := func(actor int64, action string) int {
		sess := store.Create(session.Principal{Role: auth.RoleAdmin, ID: actor})
		body, _ := json.Marshal(map[string]any{"id": strconv.FormatInt(id, 10), "action": action, "note": "synthetic acceptance exercise"})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/care/transition", bytes.NewReader(body))
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
		r.Header.Set(csrf.HeaderName, sess.CSRFToken)
		w := httptest.NewRecorder()
		transition.ServeHTTP(w, r)
		return w.Code
	}
	for _, action := range []string{"acknowledge", "start", "resolve"} {
		if code := act(1, action); code != 200 {
			t.Fatalf("%s HTTP %d", action, code)
		}
	}
	if act(1, "verify") != 403 {
		t.Fatal("resolver verified own incident")
	}
	if code := act(2, "verify"); code != 200 {
		t.Fatalf("second person verification HTTP %d", code)
	}
	rows, err := pool.QueryContext(ctx, "SELECT action FROM care_incident_event WHERE incident_id=? ORDER BY id", id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if fmt.Sprint(actions) != "[received delivered acknowledge start resolve verify]" {
		t.Fatalf("event trace %v", actions)
	}
	t.Logf("LOCAL REAL DB + TLS HTTP TRACE incident=%d notification=%d actions=%v; synthetic device/provider/actors, no real person contacted", id, notificationID, actions)
}

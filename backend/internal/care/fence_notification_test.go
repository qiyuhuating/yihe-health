package care

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFenceNotificationPreservesIncidentKind(t *testing.T) {
	pool := testCareDB(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "UPDATE care_incident SET kind='fence_exit' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO care_notification_outbox(incident_id,channel,recipient) VALUES(1,'sms','13800000001')"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["type"] != "care.fence_exit" {
			t.Errorf("fence was relabelled as SOS: %v", body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	d := &dispatcher{db: pool, webhookURL: server.URL, webhookToken: "test-token", httpClient: &http.Client{Timeout: time.Second}}
	if _, err := d.dispatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := pool.QueryRowContext(ctx, "SELECT status FROM care_notification_outbox").Scan(&state); err != nil || state != "sent" {
		t.Fatal(state, err)
	}
}

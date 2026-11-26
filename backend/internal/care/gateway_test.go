package care

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.DNSError{Name: "synthetic.invalid", Err: "synthetic DNS failure"}
}

func TestGatewayRetryAfterAndFailureClasses(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		header, want string
		delay        int
	}{
		{"429", 429, "120", "gateway HTTP 429", 90},
		{"5xx", 503, "", "gateway HTTP 503", 20},
		{"dns", 0, "", "gateway dns failure", 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testCareDB(t)
			ctx := context.Background()
			if _, err := pool.ExecContext(ctx, "INSERT INTO care_notification_outbox(incident_id,channel,recipient) VALUES(1,'sms','13800000001')"); err != nil {
				t.Fatal(err)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", tc.header)
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			client := server.Client()
			if tc.status == 0 {
				client = &http.Client{Transport: failingTransport{}}
			}
			d := &dispatcher{db: pool, webhookURL: server.URL, webhookToken: "synthetic-token-not-in-error-record", httpClient: client}
			if _, err := d.dispatchNext(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := d.dispatchNext(ctx); err != nil {
				t.Fatal(err)
			}
			var status, lastError string
			var attempts int
			var retry time.Time
			if err := pool.QueryRowContext(ctx, "SELECT status,last_error,attempts,retry_at FROM care_notification_outbox").Scan(&status, &lastError, &attempts, &retry); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || lastError != tc.want || attempts != 1 || time.Until(retry) < time.Duration(tc.delay)*time.Second {
				t.Fatalf("status=%s error=%s attempts=%d retry=%s", status, lastError, attempts, retry)
			}
			if tc.status != 0 && calls != 1 {
				t.Fatal("backoff was bypassed")
			}
		})
	}
}

func TestProviderRetryAfterBounds(t *testing.T) {
	now := time.Now()
	if providerRetryAfter("120", now) != 2*time.Minute || providerRetryAfter("-1", now) != 0 || providerRetryAfter("9999999999999", now) != 24*time.Hour {
		t.Fatal("Retry-After bounds")
	}
	if delay := providerRetryAfter(now.Add(3*time.Minute).UTC().Format(http.TimeFormat), now); delay < 179*time.Second || delay > 3*time.Minute {
		t.Fatal("HTTP-date Retry-After")
	}
}

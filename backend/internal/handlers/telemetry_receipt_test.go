package handlers

import (
	"context"
	"net/http"
	"testing"
)

func TestReportPayloadDigestIsCanonical(t *testing.T) {
	first := map[string]any{"deviceNo": "WATCH001", "hr": 70, "step": 12}
	reordered := map[string]any{"step": 12, "deviceNo": "WATCH001", "hr": 70}
	a, err := reportPayloadDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := reportPayloadDigest(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) != 64 {
		t.Fatalf("digest not canonical: %q vs %q", a, b)
	}
	altered := map[string]any{"deviceNo": "WATCH001", "hr": 71, "step": 12}
	c, err := reportPayloadDigest(altered)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("changed payload produced the same digest")
	}
}

func numberedReportPayload(requestID string) map[string]any {
	return map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"bindingVersion": 1, "hr": 74, "step": 12,
		"requestId": requestID,
	}
}

func TestNumberedReportReplayReturnsCommittedAck(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	payload := numberedReportPayload("receipt-replay-001")
	first := api(t, h.deviceReport, payload)
	if first["duplicate"] == true {
		t.Fatalf("first request marked duplicate: %v", first)
	}
	second := api(t, h.deviceReport, payload)
	if second["duplicate"] != true {
		t.Fatalf("replay did not return the committed acknowledgement: %v", second)
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM ratepre").Scan(&count); err != nil || count != 1 {
		t.Fatalf("replay wrote %d samples, err=%v", count, err)
	}
}

func TestNumberedReportRejectsAlteredPayload(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	api(t, h.deviceReport, numberedReportPayload("receipt-altered-001"))
	altered := numberedReportPayload("receipt-altered-001")
	altered["hr"] = 140
	rec := callAPI(h.deviceReport, altered)
	if rec.Code != http.StatusConflict {
		t.Fatalf("ID reuse with different payload accepted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnnumberedReportKeepsLegacyBehaviour(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	payload := map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"bindingVersion": 1, "hr": 74, "step": 12,
	}
	first := api(t, h.deviceReport, payload)
	second := api(t, h.deviceReport, payload)
	if first["duplicate"] == true || second["duplicate"] == true {
		t.Fatalf("unnumbered report unexpectedly deduplicated: %v / %v", first, second)
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM device_report_receipt").Scan(&count); err != nil || count != 0 {
		t.Fatalf("unnumbered report stored %d receipts, err=%v", count, err)
	}
}

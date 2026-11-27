//go:build yihe_next_gateway

package handlers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/health"
)

func nextGatewayBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Fatal("YIHE_NEXT_GATEWAY_BINARY is required for differential tests")
	}
	return binary
}

func TestNextGatewayNativeOracle(t *testing.T) {
	var input bytes.Buffer
	var expected []any
	encode := json.NewEncoder(&input)
	add := func(kind string, body, want any) {
		t.Helper()
		if err := encode.Encode(map[string]any{"kind": kind, "body": body}); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, want)
	}
	// Run the actual Go parser, not a second hand-maintained Go specification.
	base := map[string]any{"deviceNo": "WATCH001", "secret": "synthetic", "bindingVersion": 1, "requestId": "event-1"}
	for _, c := range []struct {
		field  string
		values []any
	}{
		{"deviceNo", []any{nil, "", "  ", " abc ", strings.Repeat("表", 32), strings.Repeat("表", 33), 123, true}},
		{"secret", []any{nil, "", " ", strings.Repeat("a", 128), strings.Repeat("a", 129), true}},
		{"bindingVersion", []any{nil, 0, -1, 1, 1.5, 1e6, " 1 ", "+1", "01", "1.0", "9223372036854775807", "9223372036854775808", true}},
		{"requestId", []any{nil, "", "a:b_C.d-1", "bad/id", "bad id", "设备", strings.Repeat("a", 64), strings.Repeat("a", 65), 123, 1.25, true}},
		{"alarm", []any{nil, "", "FALL", "1", "2", "falling", true}},
		{"hr", []any{nil, 0, 251, strings.Repeat("1", 12), strings.Repeat("1", 13)}},
		{"sleepState", []any{nil, "0", "1", "2", "10", true}},
		{"lng", []any{nil, 120.5, "120.5", strings.Repeat("1", 21)}},
		{"beTime", []any{nil, "2026-11-01 12:30:00", "2026-11-01T12:30:00Z"}},
		{"battery", []any{nil, 100, 101, 1000}},
		{"appVersion", []any{nil, "1.2.3", strings.Repeat("版", 20), strings.Repeat("版", 21)}},
	} {
		for _, value := range c.values {
			m := make(map[string]any, len(base)+1)
			for k, v := range base {
				m[k] = v
			}
			m[c.field] = value
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err = json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			_, ok := parseSOSRequest(httptest.NewRecorder(), decoded)
			add("sos", decoded, ok)
		}
	}
	// Existing Go health.Aggregate is the reference for admitted, sorted,
	// committed instantaneous heart-rate samples.
	for _, values := range [][]float64{nil, {70}, {70, 80, 90}, {1.25, 73.125, 85.5}, {70, 70, 90, 90}, {0, 0, 220}, {1e308, 1e308}} {
		var samples []health.Sample
		points := make([][2]any, 0, len(values))
		for i, v := range values {
			at := time.Unix(0, int64(i+1))
			samples = append(samples, health.Sample{Source: "watch", SourceDevice: "synthetic", Metric: "heart_rate", Unit: "bpm", Semantics: "instant", Start: at, End: at, Value: health.Number(v)})
			points = append(points, [2]any{int64(i + 1), v})
		}
		stats, err := health.Aggregate(samples, time.Unix(0, 0), time.Unix(0, 100))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"count": float64(stats.Count)}
		if stats.Count > 0 {
			want["min"] = *stats.Min
			want["max"] = *stats.Max
			want["mean"] = *stats.Mean
		}
		add("core", map[string]any{"resident_id": 1, "revision": 1, "from": 0, "to": 100, "points": points}, want)
	}
	cmd := exec.CommandContext(t.Context(), nextGatewayBinary(t), "--oracle")
	cmd.Stdin = &input
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Rust oracle: %v", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for i, want := range expected {
		if !scanner.Scan() {
			t.Fatalf("missing oracle case %d", i)
		}
		var got any
		if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("oracle case %d: got %v want %v", i, got, want)
		}
	}
	if scanner.Scan() {
		t.Fatal("unexpected extra oracle output")
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d native Rust/Go differential cases matched", len(expected))
}

func TestNextGatewayHTTPDifferential(t *testing.T) {
	t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "0")
	t.Setenv("YIHE_DURABLE_TELEMETRY", "0")
	directDB, relayDB := testDatabase(t), testDatabase(t)
	seedDevice(t, directDB)
	seedDevice(t, relayDB)
	serve := func(h *Handler) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/v1/device/report", h.deviceReport)
		mux.HandleFunc("POST /api/v1/device/sos", h.deviceSos)
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)
		return server
	}
	direct := serve(testHandler(t, directDB))
	upstream := serve(testHandler(t, relayDB))
	cmd := exec.CommandContext(t.Context(), nextGatewayBinary(t), "--serve", "127.0.0.1:0", strings.TrimPrefix(upstream.URL, "http://"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	gateway := "http://" + strings.TrimSpace(address)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	post := func(url, path string, raw []byte, headers http.Header) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header = headers.Clone()
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode HTTP %d: %v", resp.StatusCode, err)
		}
		// The Go clock is wall time at each call; these requests execute
		// sequentially against two independent schemas.
		delete(result, "serverTime")
		return resp.StatusCode, result
	}
	cases := 0
	compare := func(path string, raw []byte, headers http.Header, want int) {
		t.Helper()
		a, x := post(direct.URL, path, raw, headers)
		b, y := post(gateway, path, raw, headers)
		if a != want || b != a || !reflect.DeepEqual(x, y) {
			t.Fatalf("%s: direct %d %v, Rust relay %d %v, expected %d", path, a, x, b, y, want)
		}
		cases++
	}
	report, sos := "/api/v1/device/report", "/api/v1/device/sos"
	for _, raw := range []string{"", "{", "[]", "null", "{\"deviceNo\":\"WATCH001\"}", "{} {}"} {
		compare(report, []byte(raw), http.Header{}, 400)
	}
	base := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1}
	encode := func(extra map[string]any) []byte {
		t.Helper()
		m := make(map[string]any)
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		raw, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	compare(report, encode(map[string]any{"hr": 251}), http.Header{}, 400)
	compare(sos, encode(map[string]any{"requestId": "bad/id"}), http.Header{}, 400)
	t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "1")
	sign := func(raw []byte, nonce string) http.Header {
		ts := fmt.Sprint(time.Now().Unix())
		digest := sha256.Sum256(raw)
		mac := hmac.New(sha256.New, []byte(testSecret))
		fmt.Fprintf(mac, "WATCH001:%s:%s:%s", ts, nonce, hex.EncodeToString(digest[:]))
		h := make(http.Header)
		h.Set("X-Device-Signature", hex.EncodeToString(mac.Sum(nil)))
		h.Set("X-Device-Timestamp", ts)
		h.Set("X-Device-Nonce", nonce)
		h.Set("Content-Type", "application/json")
		return h
	}
	raw := encode(map[string]any{"hr": 72, "beTime": "2026-11-01 12:30:00"})
	compare(report, raw, http.Header{}, 401)
	bad := sign(raw, "next-invalid-signature-123")
	bad.Set("X-Device-Signature", "00")
	compare(report, raw, bad, 401)
	h := sign(raw, "next-telemetry-replay-123")
	compare(report, raw, h, 200)
	compare(report, raw, h, 409)
	raw = encode(map[string]any{"requestId": "next-sos-1"})
	h = sign(raw, "next-sos-replay-123456789")
	compare(sos, raw, h, 200)
	compare(sos, raw, h, 200)
	for _, query := range []string{"SELECT COUNT(*) FROM ratepre WHERE device_id=1", "SELECT COUNT(*) FROM care_incident", "SELECT COUNT(*) FROM device_request_nonces"} {
		var a, b int
		if err := directDB.QueryRowContext(context.Background(), query).Scan(&a); err != nil {
			t.Fatal(err)
		}
		if err := relayDB.QueryRowContext(context.Background(), query).Scan(&b); err != nil {
			t.Fatal(err)
		}
		if a != b || a == 0 {
			t.Fatalf("database effects differ: %s %d/%d", query, a, b)
		}
	}
	resp, err := client.Get(gateway + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("Rust liveness: %d", resp.StatusCode)
	}
	t.Logf("%d HTTP response comparisons and 3 durable table comparisons matched", cases)
}

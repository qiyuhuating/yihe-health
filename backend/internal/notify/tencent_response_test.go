package notify

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type smsStubTransport func(*http.Request) (*http.Response, error)

func (f smsStubTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTencentRequiresExplicitBoundedProviderAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"accepted", `{"result":0}`, 200, true},
		{"missing_result", `{}`, 200, false},
		{"rejected", `{"result":1}`, 200, false},
		{"not_ok", `{"result":0}`, 429, false},
		{"trailing_json", `{"result":0} {}`, 200, false},
		{"oversized", strings.Repeat(" ", 64<<10) + `{"result":0}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: smsStubTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "yun.tim.qq.com" || r.Method != http.MethodPost {
					t.Fatalf("wrong SMS target: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			err := TencentSMS(context.Background(), client, "123456", "unit-test-key", "13800138000")
			if (err == nil) != tc.ok {
				t.Fatalf("success=%v want=%v", err == nil, tc.ok)
			}
		})
	}
}

func TestTencentConfigFileDisablesBulkAdapter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sms-appid")
	if err := os.WriteFile(path, []byte("123456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("TENCENT_SMS_APPID_FILE", path)
	if (&Worker{BulkURL: "https://bulk.example.test"}).bulkEnabled() {
		t.Fatal("configured SMS provider must not be bypassed")
	}
}

package care

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestGatewayTransportRequiresTLS12WithoutCustomCA(t *testing.T) {
	client, err := gatewayClient("")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil {
		t.Fatal("missing explicit TLS configuration")
	}
	if transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Fatal("gateway allows TLS versions below 1.2")
	}
	if transport.MaxConnsPerHost != 32 || transport.MaxIdleConnsPerHost != 32 || transport.ResponseHeaderTimeout != 10*time.Second {
		t.Fatal("gateway transport lacks bounded resource settings")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy: %v", err)
	}
	transport.CloseIdleConnections()
}

package care

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type gatewayFailure struct {
	reason     string
	status     int
	retryAfter time.Duration
}

func (e *gatewayFailure) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("gateway HTTP %d", e.status)
	}
	return "gateway " + e.reason + " failure"
}

func transportFailure(err error) error {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return &gatewayFailure{reason: "dns"}
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return &gatewayFailure{reason: "timeout"}
	}
	return &gatewayFailure{reason: "network"}
}
func providerRetryAfter(raw string, now time.Time) time.Duration {
	var delay time.Duration
	if seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
		if seconds > 86400 {
			seconds = 86400
		}
		if seconds > 0 {
			delay = time.Duration(seconds) * time.Second
		}
	} else if date, err := http.ParseTime(raw); err == nil {
		delay = date.Sub(now)
	}
	return min(24*time.Hour, max(0, delay))
}

func gatewayClient(caFile string) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("gateway transport is unavailable")
	}
	transport = transport.Clone()
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 32
	transport.MaxConnsPerHost = 32
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 10 * time.Second
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, errors.New("gateway CA file is unavailable")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("gateway CA file contains no valid certificate")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

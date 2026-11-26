package ratelimit

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// NewClientIPResolver shares the exact trusted-hop policy with audit/auth code.
// It creates no limiter state or background worker.
func NewClientIPResolver(cidrs []string) (func(*http.Request) string, error) {
	_, trusted, err := normalized(Options{TrustedProxyCIDRs: cidrs})
	if err != nil {
		return nil, err
	}
	resolver := &Limiter{trusted: trusted}
	return resolver.clientIP, nil
}

func parseAddress(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "\"") {
		decoded, err := strconv.Unquote(raw)
		if err != nil {
			return netip.Addr{}, false
		}
		raw = decoded
	}
	if addr, err := netip.ParseAddr(strings.Trim(raw, "[]")); err == nil && addr.Zone() == "" {
		return addr.Unmap(), true
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil && addr.Zone() == "" {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func (l *Limiter) isTrusted(ip netip.Addr) bool {
	for _, prefix := range l.trusted {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (l *Limiter) clientIP(r *http.Request) string {
	peer, ok := parseAddress(r.RemoteAddr)
	if !ok {
		return "invalid-peer"
	}
	if !l.isTrusted(peer) {
		return peer.String()
	}
	forwarded := r.Header.Values("Forwarded")
	header := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if len(forwarded) != 0 {
		header = strings.Join(forwarded, ",")
	}
	if len(header) == 0 || len(header) > 4096 {
		return peer.String()
	}
	parts := strings.Split(header, ",")
	if len(parts) > 32 {
		return peer.String()
	}
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if len(forwarded) != 0 {
			value = ""
			for _, param := range strings.Split(part, ";") {
				name, v, found := strings.Cut(strings.TrimSpace(param), "=")
				if found && strings.EqualFold(name, "for") {
					if value != "" {
						return peer.String()
					}
					value = strings.TrimSpace(v)
				}
			}
		}
		addr, valid := parseAddress(value)
		if !valid {
			return peer.String()
		}
		chain = append(chain, addr)
	}
	client := peer
	for i := len(chain) - 1; i >= 0 && l.isTrusted(client); i-- {
		client = chain[i]
	}
	return client.String()
}

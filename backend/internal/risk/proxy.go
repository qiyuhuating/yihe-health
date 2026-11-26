package risk

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// intelligenceSnapshot is immutable after publication. Request handling never
// reads intelligence files or waits for a feed refresh to finish.
type intelligenceSnapshot struct {
	signals map[netip.Prefix]NetworkSignal
	legacy  map[netip.Prefix]struct{}
}

func (m *Manager) publishIntelligence(signals map[netip.Prefix]NetworkSignal, legacy map[netip.Prefix]struct{}) {
	for {
		old := m.intelligence.Load()
		next := &intelligenceSnapshot{signals: signals, legacy: legacy}
		if old != nil {
			if signals == nil {
				next.signals = old.signals
			}
			if legacy == nil {
				next.legacy = old.legacy
			}
		}
		if m.intelligence.CompareAndSwap(old, next) {
			return
		}
	}
}

// StartNetworkRefresh builds new snapshots off the request path. The caller
// owns cancellation; invalid refreshes leave the last known-good data active.
// A failed initial load is reported synchronously to allow startup diagnostics.
func (m *Manager) StartNetworkRefresh(ctx context.Context, path string, interval time.Duration, onError func(error)) error {
	if interval <= 0 {
		return fmt.Errorf("refresh interval must be positive")
	}
	if err := m.LoadNetworkSignals(path); err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.LoadNetworkSignals(path); err != nil && onError != nil {
					onError(err)
				}
			}
		}
	}()
	return nil
}

// LoadProxyCIDRs takes a locally curated IP-intelligence feed, never a client header.
// Atomically replaces the list; callers can refresh it when their trusted feed updates.
func (m *Manager) LoadProxyCIDRs(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	prefixes := make(map[netip.Prefix]struct{})
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(strings.SplitN(scan.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		p, err := normalizedPrefix(line)
		if err != nil {
			return fmt.Errorf("invalid network feed entry: %w", err)
		}
		prefixes[p] = struct{}{}
		if len(prefixes) > 100000 {
			return fmt.Errorf("network feed too large")
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	m.publishIntelligence(nil, prefixes)
	return nil
}

// NetworkSignal is an imported and attributable IP intelligence observation.
// File columns: CIDR, kind, confidence(0-100), observedAt(YYYY-MM-DD), provider, source.
// Supported kinds: vpn, open_proxy, tor, residential_proxy, mobile_proxy,
// hosting, private_relay, enterprise_proxy, unknown.
type NetworkSignal struct {
	Prefix     netip.Prefix
	Kind       string
	Confidence int
	ObservedAt time.Time
	Provider   string
	Source     string
}

type NetworkClassification struct {
	Kind       string
	Confidence int
	Provider   string
	Source     string
}

func validKind(s string) bool {
	switch s {
	case "vpn", "open_proxy", "tor", "residential_proxy", "mobile_proxy", "hosting", "private_relay", "enterprise_proxy", "unknown":
		return true
	}
	return false
}

// LoadNetworkSignals imports a curated vendor feed. Invalid updates are rejected
// atomically, retaining the last known-good classification database.
func (m *Manager) LoadNetworkSignals(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = 6
	rows := make(map[netip.Prefix]NetworkSignal)
	for line := 1; ; line++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("network feed line %d: %w", line, err)
		}
		if line == 1 && strings.EqualFold(strings.TrimSpace(record[0]), "cidr") {
			continue
		}
		prefix, err := normalizedPrefix(strings.TrimSpace(record[0]))
		if err != nil {
			return fmt.Errorf("network feed line %d: %w", line, err)
		}
		kind := strings.TrimSpace(record[1])
		if !validKind(kind) {
			return fmt.Errorf("network feed line %d: unknown kind", line)
		}
		confidence, err := strconv.Atoi(strings.TrimSpace(record[2]))
		if err != nil || confidence < 0 || confidence > 100 {
			return fmt.Errorf("network feed line %d: invalid confidence", line)
		}
		observed, err := time.Parse("2006-01-02", strings.TrimSpace(record[3]))
		if err != nil {
			return fmt.Errorf("network feed line %d: invalid observed date", line)
		}
		provider, source := strings.TrimSpace(record[4]), strings.TrimSpace(record[5])
		if len(provider) > 100 || len(source) > 100 || source == "" {
			return fmt.Errorf("network feed line %d: invalid attribution", line)
		}
		rows[prefix] = NetworkSignal{Prefix: prefix, Kind: kind, Confidence: confidence, ObservedAt: observed, Provider: provider, Source: source}
		if len(rows) > 250000 {
			return fmt.Errorf("network feed too large")
		}
	}
	m.publishIntelligence(rows, nil)
	return nil
}

func (m *Manager) classifyNetworkLocked(ip string) NetworkClassification {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return NetworkClassification{Kind: "unknown"}
	}
	addr = addr.Unmap()
	snapshot := m.intelligence.Load()
	if snapshot == nil {
		return NetworkClassification{Kind: "unclassified"}
	}
	// At most 129 prefix lookups. Lookup is lock-free, no file access, DNS,
	// network operation, allocation-heavy filter or vendor call.
	now := m.now()
	for bits := addr.BitLen(); bits >= 0; bits-- {
		prefix := netip.PrefixFrom(addr, bits).Masked()
		if row, found := snapshot.signals[prefix]; found {
			age := now.Sub(row.ObservedAt)
			if age >= -24*time.Hour && age <= 30*24*time.Hour {
				return NetworkClassification{Kind: row.Kind, Confidence: row.Confidence, Provider: row.Provider, Source: row.Source}
			}
		}
		if _, found := snapshot.legacy[prefix]; found {
			return NetworkClassification{Kind: "vpn_or_proxy", Confidence: 100, Source: "legacy-curated-cidr"}
		}
	}
	return NetworkClassification{Kind: "unclassified"}
}

// Network categorization is not protocol inspection: HTTPS hides tunnel type.
// Do not block all datacenter, enterprise or relay traffic as "VPN".
func (m *Manager) shouldBlockNetworkLocked(ip string) bool {
	signal := m.classifyNetworkLocked(ip)
	switch signal.Kind {
	case "vpn_or_proxy":
		return true
	case "vpn", "tor", "open_proxy":
		return signal.Confidence >= 80
	case "residential_proxy", "mobile_proxy":
		return signal.Confidence >= 90
	default:
		return false
	}
}

// networkClassLocked remains for legacy references and admin review.
func (m *Manager) networkClassLocked(ip string) string { return m.classifyNetworkLocked(ip).Kind }

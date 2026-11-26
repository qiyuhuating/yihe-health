// Package risk limits verified authentication failures without trusting browser reports.
package risk

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	window          = 10 * time.Minute
	accountLimit    = 5
	accountCooldown = 5 * time.Minute
	ipLimit         = 30
	ipCooldown      = 2 * time.Minute
	inviteLimit     = 12
	inviteCooldown  = 2 * time.Minute
	maxEntries      = 16384
)

type entry struct {
	failures            int
	since, blockedUntil time.Time
}

type Manager struct {
	mu              sync.Mutex
	ruleMu          sync.Mutex
	entries         map[string]entry
	now             func() time.Time
	rules           []Rule
	events          []Event
	storage         string
	intelligence    atomic.Pointer[intelligenceSnapshot]
	diskMu          sync.Mutex
	generation      uint64
	savedGeneration uint64
	auditWake       chan struct{}
	auditCancel     context.CancelFunc
	auditDone       chan struct{}
	sqlAudit        *mysqlAudit
	bootID          string
	eventSeq        atomic.Uint64
}

func New() *Manager {
	var seed [8]byte
	if _, err := rand.Read(seed[:]); err != nil {
		panic("risk identity entropy unavailable: " + err.Error())
	}
	return &Manager{entries: make(map[string]entry), now: time.Now, bootID: fmt.Sprintf("%x", seed[:])}
}
func (m *Manager) nextEventID() string { return fmt.Sprintf("%s%016x", m.bootID, m.eventSeq.Add(1)) }

// Decision is derived only from server-observed authentication outcomes.
// A retryAfter of zero permits the next attempt.
type Decision struct {
	RetryAfter int    `json:"retryAfter"`
	RiskLevel  string `json:"riskLevel"`
}

func (d Decision) Allowed() bool { return d.RetryAfter == 0 }

func key(kind, value string) string {
	hash := sha256.Sum256([]byte(value))
	return kind + ":" + hex.EncodeToString(hash[:])
}

func remaining(e entry, now time.Time) int {
	if !now.Before(e.blockedUntil) {
		return 0
	}
	return int((e.blockedUntil.Sub(now) + time.Second - 1) / time.Second)
}

func (m *Manager) checkLocked(keys ...string) Decision {
	now := m.now()
	for _, k := range keys {
		if n := remaining(m.entries[k], now); n > 0 {
			return Decision{RetryAfter: n, RiskLevel: "blocked"}
		}
	}
	return Decision{RiskLevel: "low"}
}

// BeforeLogin checks account+IP and IP budgets. A shared campus NAT is not
// locked for one account's failures; it needs 30 verified failures in 10 minutes.
func (m *Manager) BeforeLogin(ip, account string) Decision {
	if err := m.refreshMySQLRules(); err != nil {
		return Decision{RetryAfter: 1, RiskLevel: "storage_unavailable"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, matched := m.matchRuleLocked(ip); matched {
		return d
	}
	if m.shouldBlockNetworkLocked(ip) {
		return Decision{RetryAfter: 60, RiskLevel: "proxy"}
	}
	return m.checkLocked(key("login-account-ip", ip+"\x00"+account), key("login-ip", ip))
}

func (m *Manager) BeforeInvite(ip string) Decision {
	if err := m.refreshMySQLRules(); err != nil {
		return Decision{RetryAfter: 1, RiskLevel: "storage_unavailable"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, matched := m.matchRuleLocked(ip); matched {
		return d
	}
	if m.shouldBlockNetworkLocked(ip) {
		return Decision{RetryAfter: 60, RiskLevel: "proxy"}
	}
	return m.checkLocked(key("invite-ip", ip))
}

func (m *Manager) recordLocked(k string, threshold int, cooldown time.Duration) Decision {
	now := m.now()
	e := m.entries[k]
	if n := remaining(e, now); n > 0 {
		return Decision{RetryAfter: n, RiskLevel: "blocked"}
	}
	if e.since.IsZero() || now.Sub(e.since) >= window {
		e = entry{since: now}
	}
	e.failures++
	if e.failures >= threshold {
		e.blockedUntil = now.Add(cooldown)
	}
	if _, exists := m.entries[k]; !exists && len(m.entries) >= maxEntries {
		// Never silently evict a live penalty under memory pressure.
		for k, v := range m.entries {
			if !now.Before(v.blockedUntil) && (v.since.IsZero() || now.Sub(v.since) >= window) {
				delete(m.entries, k)
			}
		}
		if len(m.entries) >= maxEntries {
			// Do not admit more unbounded keys; outer route limiter still applies.
			return Decision{RiskLevel: "high"}
		}
	}
	m.entries[k] = e
	if n := remaining(e, now); n > 0 {
		return Decision{RetryAfter: n, RiskLevel: "blocked"}
	}
	return Decision{RiskLevel: "low"}
}

func (m *Manager) FailedLogin(ip, account string, signals ...string) Decision {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordEventLocked(ip, account, "login_failed", "invalid_credentials", signals...)
	a := m.recordLocked(key("login-account-ip", ip+"\x00"+account), accountLimit, accountCooldown)
	b := m.recordLocked(key("login-ip", ip), ipLimit, ipCooldown)
	if b.RetryAfter > a.RetryAfter {
		return b
	}
	return a
}

func (m *Manager) SuccessfulLogin(ip, account string, signals ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key("login-account-ip", ip+"\x00"+account))
	m.recordEventLocked(ip, account, "login_success", "authenticated", signals...)
	// Keep IP-wide failure evidence; a single valid account must not erase stuffing signals.
}

func (m *Manager) InvalidInvite(ip string, signals ...string) Decision {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordEventLocked(ip, "", "invite_failed", "invalid_code", signals...)
	return m.recordLocked(key("invite-ip", ip), inviteLimit, inviteCooldown)
}

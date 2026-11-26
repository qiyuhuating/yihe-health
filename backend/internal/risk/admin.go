package risk

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Rule is a managed IP/CIDR block. It is separate from temporary automatic cool-downs.
type Rule struct {
	ID        string    `json:"id"`
	CIDR      string    `json:"cidr"`
	Action    string    `json:"action"` // block; skip-auto
	Reason    string    `json:"reason"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

var ErrInvalidRule = errors.New("invalid risk rule")

type Event struct {
	ID                string     `json:"id"`
	At                time.Time  `json:"at"`
	IP                string     `json:"ip"`
	Account           string     `json:"account"` // masked phone; no passwords or tokens
	Type              string     `json:"type"`
	Detail            string     `json:"detail"`
	Device            DeviceInfo `json:"device"`
	Network           string     `json:"network"`
	NetworkConfidence int        `json:"networkConfidence,omitempty"`
	NetworkProvider   string     `json:"networkProvider,omitempty"`
	NetworkSource     string     `json:"networkSource,omitempty"`
}

func masked(account string) string {
	if len(account) < 7 {
		return "***"
	}
	return account[:3] + "****" + account[len(account)-4:]
}

func normalizedPrefix(s string) (netip.Prefix, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, errors.New("invalid IP or CIDR")
	}
	return p.Masked(), nil
}

// Persist uses a private JSON snapshot, shared by restarts of a single process.
// Use a shared database instead if multiple server replicas will be deployed.
func (m *Manager) Persist(dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	m.storage = filepath.Join(dir, "risk-security.json")
	b, err := os.ReadFile(m.storage)
	if os.IsNotExist(err) {
		ctx, cancel := context.WithCancel(context.Background())
		m.auditWake = make(chan struct{}, 1)
		m.auditDone = make(chan struct{})
		m.auditCancel = cancel
		go m.auditWorker(ctx)
		return nil
	}
	if err != nil {
		return err
	}
	var v struct {
		Rules  []Rule  `json:"rules"`
		Events []Event `json:"events"`
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("load risk history: %w", err)
	}
	m.rules = v.Rules
	m.events = v.Events
	ctx, cancel := context.WithCancel(context.Background())
	m.auditWake = make(chan struct{}, 1)
	m.auditDone = make(chan struct{})
	m.auditCancel = cancel
	go m.auditWorker(ctx)
	return nil
}

type auditSnapshot struct {
	Rules  []Rule  `json:"rules"`
	Events []Event `json:"events"`
}

// Called while m.mu is held; it copies mutable slices before releasing the lock.
func (m *Manager) snapshotLocked() (auditSnapshot, uint64) {
	return auditSnapshot{Rules: append([]Rule(nil), m.rules...), Events: append([]Event(nil), m.events...)}, m.generation
}

func (m *Manager) writeSnapshot(s auditSnapshot, gen uint64) error {
	if m.storage == "" {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	m.diskMu.Lock()
	defer m.diskMu.Unlock()
	if gen <= m.savedGeneration {
		return nil
	}
	// The temporary file is unique to this writer and permissions are private.
	tmp := m.storage + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, m.storage); err != nil {
		return err
	}
	m.savedGeneration = gen
	return nil
}

// saveLocked is reserved for low-frequency administrator rule mutations.
// Authentication attempts never call it.
func (m *Manager) saveLocked() error {
	s, gen := m.snapshotLocked()
	return m.writeSnapshot(s, gen)
}

func (m *Manager) notifyAuditLocked() {
	if m.auditWake != nil {
		select {
		case m.auditWake <- struct{}{}:
		default:
		}
	}
}

// Flush forces pending events to disk, for shutdown or integration tests.
func (m *Manager) Flush() error {
	m.mu.Lock()
	snapshot, gen := m.snapshotLocked()
	m.mu.Unlock()
	return m.writeSnapshot(snapshot, gen)
}

// Close flushes pending audit events and stops the writer.
// Call at server shutdown after requests have drained.
func (m *Manager) Close() error {
	m.mu.Lock()
	cancel, done := m.auditCancel, m.auditDone
	m.auditCancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return m.Flush()
}

func (m *Manager) auditWorker(ctx context.Context) {
	defer close(m.auditDone)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	dirty := false
	for {
		select {
		case <-m.auditWake:
			dirty = true
		case <-ticker.C:
			if dirty {
				if err := m.Flush(); err != nil {
					log.Printf("risk audit persistence error: %v", err)
				} else {
					dirty = false
				}
			}
		case <-ctx.Done():
			if err := m.Flush(); err != nil {
				log.Printf("risk audit shutdown persistence error: %v", err)
			}
			return
		}
	}
}

func (m *Manager) recordEventLocked(ip, account, typ, detail string, signals ...string) {
	// Never persist passwords, cookies, headers, or raw phone numbers.
	e := Event{ID: m.nextEventID(), At: m.now().UTC(), IP: ip, Account: masked(account), Type: typ, Detail: detail, Device: IdentifyDevice(signals...)}
	classification := m.classifyNetworkLocked(ip)
	e.Network, e.NetworkConfidence, e.NetworkProvider, e.NetworkSource = classification.Kind, classification.Confidence, classification.Provider, classification.Source
	// In MySQL mode the bounded queue is the only hot-path audit buffer.
	// Avoid maintaining and copying a second 2,000-element JSON history on
	// every login. Durable history is read directly from MySQL by administrators.
	if m.sqlAudit != nil {
		m.sqlAudit.push(e)
		return
	}
	m.events = append(m.events, e)
	// Last 2000 entries, maximum 30 days. This limits disk use and retention.
	cutoff := m.now().Add(-30 * 24 * time.Hour)
	first := 0
	for first < len(m.events) && m.events[first].At.Before(cutoff) {
		first++
	}
	if len(m.events)-first > 2000 {
		first = len(m.events) - 2000
	}
	m.events = append([]Event(nil), m.events[first:]...)
	m.generation++
	m.notifyAuditLocked()
}

func (m *Manager) RecordDenied(ip, account, detail string, signals ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordEventLocked(ip, account, "request_blocked", detail, signals...)
}

func (m *Manager) matchRuleLocked(ip string) (Decision, bool) {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return Decision{}, false
	}
	a = a.Unmap()
	now := m.now()
	// Block takes precedence over skip-auto; explicit exceptions only bypass auto throttling.
	skip := false
	for _, rule := range m.rules {
		if !rule.ExpiresAt.IsZero() && !now.Before(rule.ExpiresAt) {
			continue
		}
		p, err := normalizedPrefix(rule.CIDR)
		if err != nil || !p.Contains(a) {
			continue
		}
		if rule.Action == "block" {
			retry := 3600
			if !rule.ExpiresAt.IsZero() {
				retry = int(rule.ExpiresAt.Sub(now).Seconds() + 1)
			}
			return Decision{RetryAfter: retry, RiskLevel: "blocked"}, true
		}
		if rule.Action == "skip-auto" {
			skip = true
		}
	}
	if skip {
		return Decision{RiskLevel: "low"}, true
	}
	return Decision{}, false
}

// InsertRuleTx uses the same database transaction as the live-duty authorization.
func (m *Manager) InsertRuleTx(ctx context.Context, tx *sql.Tx, raw, action, reason, actor string, ttl time.Duration) (Rule, error) {
	prefix, err := normalizedPrefix(strings.TrimSpace(raw))
	if err != nil {
		return Rule{}, ErrInvalidRule
	}
	if action != "block" && action != "skip-auto" {
		return Rule{}, ErrInvalidRule
	}
	if ttl < 0 || ttl > 365*24*time.Hour {
		return Rule{}, ErrInvalidRule
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 240 {
		return Rule{}, ErrInvalidRule
	}
	now := m.now().UTC()
	rule := Rule{ID: m.nextEventID(), CIDR: prefix.String(), Action: action, Reason: reason, Actor: actor, CreatedAt: now}
	if ttl > 0 {
		rule.ExpiresAt = now.Add(ttl)
	}
	var expires any
	if !rule.ExpiresAt.IsZero() {
		expires = rule.ExpiresAt
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO risk_ip_rules (rule_id,cidr,action,reason,actor,created_at,expires_at) VALUES (?,?,?,?,?,?,?)`, rule.ID, rule.CIDR, rule.Action, rule.Reason, rule.Actor, rule.CreatedAt, expires); err != nil {
		return Rule{}, err
	}
	if err = m.auditRuleTx(ctx, tx, "rule_created", rule.CIDR, actor, action+": "+reason); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// RemoveRuleTx locks the persisted rule so deletion and audit commit together.
func (m *Manager) RemoveRuleTx(ctx context.Context, tx *sql.Tx, id, actor string) (bool, error) {
	var cidr string
	err := tx.QueryRowContext(ctx, "SELECT cidr FROM risk_ip_rules WHERE rule_id=? FOR UPDATE", id).Scan(&cidr)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM risk_ip_rules WHERE rule_id=?", id); err != nil {
		return false, err
	}
	if err = m.auditRuleTx(ctx, tx, "rule_deleted", cidr, actor, id); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Manager) auditRuleTx(ctx context.Context, tx *sql.Tx, action, cidr, actor, detail string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO risk_login_events
 (event_key,occurred_at,ip_address,account_mask,event_type,detail,device_json,network_kind,network_confidence,network_provider,network_source)
	 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, m.nextEventID(), m.now().UTC(), cidr, actor, action, detail, "{}", "unclassified", 0, "", "")
	return err
}

// ReloadRules publishes only database-committed rules.
func (m *Manager) ReloadRules(ctx context.Context) error {
	m.ruleMu.Lock()
	defer m.ruleMu.Unlock()
	m.mu.Lock()
	a := m.sqlAudit
	m.mu.Unlock()
	if a == nil {
		return errors.New("risk MySQL persistence unavailable")
	}
	rules, err := loadSQLRules(ctx, a.db)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.rules = rules
	m.mu.Unlock()
	return nil
}

func (m *Manager) AddRule(raw, action, reason, actor string, ttl time.Duration) (Rule, error) {
	prefix, err := normalizedPrefix(strings.TrimSpace(raw))
	if err != nil {
		return Rule{}, err
	}
	if action != "block" && action != "skip-auto" {
		return Rule{}, errors.New("invalid action")
	}
	if ttl < 0 || ttl > 365*24*time.Hour {
		return Rule{}, errors.New("invalid duration")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 240 {
		return Rule{}, errors.New("invalid reason")
	}
	m.ruleMu.Lock()
	defer m.ruleMu.Unlock()
	now := m.now().UTC()
	rule := Rule{ID: fmt.Sprintf("%d", now.UnixNano()), CIDR: prefix.String(), Action: action, Reason: reason, Actor: actor, CreatedAt: now}
	if ttl > 0 {
		rule.ExpiresAt = now.Add(ttl)
	}
	m.mu.Lock()
	a := m.sqlAudit
	m.mu.Unlock()
	if a != nil {
		return Rule{}, errors.New("MySQL rule changes require an authorized transaction")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules, rule)
	m.generation++
	if err = m.saveLocked(); err != nil {
		m.rules = m.rules[:len(m.rules)-1]
		return Rule{}, err
	}
	m.recordEventLocked(prefix.String(), "", "rule_created", action+": "+reason)
	return rule, nil
}

func (m *Manager) DeleteRule(id, actor string) (bool, error) {
	m.ruleMu.Lock()
	defer m.ruleMu.Unlock()
	m.mu.Lock()
	index := -1
	var found Rule
	for i, r := range m.rules {
		if r.ID == id {
			index = i
			found = r
			break
		}
	}
	a := m.sqlAudit
	m.mu.Unlock()
	if a != nil {
		return false, errors.New("MySQL rule changes require an authorized transaction")
	}
	if index < 0 {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules[:index], m.rules[index+1:]...)
	m.generation++
	if err := m.saveLocked(); err != nil {
		m.rules = append(m.rules, Rule{})
		copy(m.rules[index+1:], m.rules[index:])
		m.rules[index] = found
		return false, err
	}
	m.recordEventLocked(found.CIDR, "", "rule_deleted", actor+": "+found.Reason)
	return true, nil
}
func (m *Manager) History(limit int) ([]Event, []Rule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 1 || limit > 200 {
		limit = 100
	}
	from := len(m.events) - limit
	if from < 0 {
		from = 0
	}
	events := make([]Event, len(m.events)-from)
	for i := range events {
		events[i] = m.events[len(m.events)-i-1]
	}
	rules := append([]Rule(nil), m.rules...)
	return events, rules
}

package risk

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
	"yihe-care/internal/config"
)

// mysqlAudit is one asynchronous writer for authentication observations. The
// MySQL connection pool and schema are provisioned by the host application.
type mysqlAudit struct {
	db        *sql.DB
	queue     chan Event
	stop      chan struct{}
	done      chan struct{}
	dropped   atomic.Uint64
	spoolPath string
}

func (a *mysqlAudit) push(e Event) {
	select {
	case a.queue <- e:
	default:
		a.dropped.Add(1)
	}
}

// persistBacklog atomically records events still awaiting MySQL. It executes
// only on the writer goroutine; login requests never touch this file.
func (a *mysqlAudit) persistBacklog(events []Event) error {
	if a.spoolPath == "" {
		return errors.New("risk audit spool path not configured")
	}
	if err := os.MkdirAll(filepath.Dir(a.spoolPath), 0700); err != nil {
		return err
	}
	var data bytes.Buffer
	enc := json.NewEncoder(&data)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	if data.Len() > 64<<20 {
		return fmt.Errorf("risk audit spool exceeds 64 MiB")
	}
	tmp := a.spoolPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, a.spoolPath)
}

func (a *mysqlAudit) readBacklog() ([]Event, error) {
	if a.spoolPath == "" {
		return nil, nil
	}
	b, err := os.ReadFile(a.spoolPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 64<<20 {
		return nil, fmt.Errorf("risk audit spool exceeds 64 MiB")
	}
	var events []Event
	dec := json.NewDecoder(bytes.NewReader(b))
	for dec.More() {
		var e Event
		if err := dec.Decode(&e); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func (a *mysqlAudit) run() {
	defer close(a.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	pruneTicker := time.NewTicker(time.Hour)
	defer pruneTicker.Stop()
	pending, err := a.readBacklog()
	if err != nil {
		slog.Error("risk audit spool recovery failed", "error", err)
		return
	}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		err := writeAuditBatch(ctx, a.db, pending)
		cancel()
		if err != nil {
			if saveErr := a.persistBacklog(pending); saveErr != nil {
				slog.Error("risk audit persistence unavailable", "database_error", err, "spool_error", saveErr, "pending", len(pending))
			}
			return
		}
		// Persist the empty state after confirmed commit. If this fails,
		// replay is harmless because inserts are keyed by event ID.
		if err := a.persistBacklog(nil); err != nil {
			slog.Error("risk audit spool cleanup failed", "error", err)
		}
		pending = pending[:0]
	}
	// Bound in-memory backlog when MySQL is unavailable. The channel is bounded,
	// but continuously draining it into pending would otherwise grow without limit.
	appendPending := func(e Event) {
		if len(pending) >= 4096 {
			a.dropped.Add(1)
			return
		}
		pending = append(pending, e)
	}
	for {
		select {
		case e := <-a.queue:
			appendPending(e)
			if len(pending) >= 128 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-pruneTicker.C:
			if len(pending) > 0 {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := a.db.ExecContext(ctx, `DELETE FROM risk_login_events WHERE occurred_at < UTC_TIMESTAMP() - INTERVAL 30 DAY LIMIT 10000`)
			cancel()
			if err != nil {
				slog.Error("risk audit retention prune failed", "error", err)
			}
		case <-a.stop:
			for {
				select {
				case e := <-a.queue:
					appendPending(e)
				default:
					flush()
					if len(pending) > 0 {
						if err := a.persistBacklog(pending); err != nil {
							a.dropped.Add(uint64(len(pending)))
							slog.Error("risk audit shutdown spool failed", "error", err)
						}
					}
					return
				}
			}
		}
	}
}

func writeAuditBatch(ctx context.Context, db *sql.DB, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO risk_login_events (event_key, occurred_at, ip_address, account_mask, event_type, detail, device_json, network_kind, network_confidence, network_provider, network_source) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE event_key=VALUES(event_key)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range events {
		device, err := json.Marshal(e.Device)
		if err != nil {
			return err
		}
		_, err = stmt.ExecContext(ctx, e.ID, e.At.UTC(), e.IP, e.Account, e.Type, e.Detail, string(device), e.Network, e.NetworkConfidence, e.NetworkProvider, e.NetworkSource)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func loadSQLRules(ctx context.Context, db *sql.DB) ([]Rule, error) {
	rows, err := db.QueryContext(ctx, `SELECT rule_id,cidr,action,reason,actor,created_at,expires_at FROM risk_ip_rules ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("load risk rules: %w", err)
	}
	rules := make([]Rule, 0)
	for rows.Next() {
		var r Rule
		var exp sql.NullTime
		if err = rows.Scan(&r.ID, &r.CIDR, &r.Action, &r.Reason, &r.Actor, &r.CreatedAt, &exp); err != nil {
			break
		}
		if exp.Valid {
			r.ExpiresAt = exp.Time
		}
		rules = append(rules, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// Each authentication decision reads committed rules from the shared database.
// A process-local snapshot cannot establish visibility across server replicas.
func (m *Manager) refreshMySQLRules() error {
	m.mu.Lock()
	enabled := m.sqlAudit != nil
	m.mu.Unlock()
	if !enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.ReloadRules(ctx)
}

func (m *Manager) EnableMySQL(db *sql.DB) error {
	if db == nil {
		return errors.New("nil mysql database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rules, err := loadSQLRules(ctx, db)
	if err != nil {
		return err
	}
	spoolPath := config.Setting("YIHE_RISK_AUDIT_SPOOL_PATH")
	if spoolPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("audit spool config directory: %w", err)
		}
		spoolPath = filepath.Join(configDir, "yihe-care", "risk-audit.jsonl")
	}
	a := &mysqlAudit{db: db, queue: make(chan Event, 4096), stop: make(chan struct{}), done: make(chan struct{}), spoolPath: spoolPath}
	if _, err := a.readBacklog(); err != nil {
		return fmt.Errorf("load risk audit spool: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(spoolPath), 0700); err != nil {
		return err
	}
	m.mu.Lock()
	if m.sqlAudit != nil {
		m.mu.Unlock()
		return errors.New("risk mysql already enabled")
	}
	m.rules = rules
	m.sqlAudit = a
	m.mu.Unlock()
	go a.run()
	return nil
}

// CloseMySQL drains already queued audit events. It must be called after the
// HTTP server has stopped accepting and processing requests.
func (m *Manager) CloseMySQL() uint64 {
	// Prevent concurrent administrator rule mutations from referencing a closed writer.
	m.ruleMu.Lock()
	defer m.ruleMu.Unlock()
	m.mu.Lock()
	a := m.sqlAudit
	m.sqlAudit = nil
	m.mu.Unlock()
	if a == nil {
		return 0
	}
	close(a.stop)
	<-a.done
	return a.dropped.Load()
}

func (m *Manager) AuditDropped() uint64 {
	m.mu.Lock()
	a := m.sqlAudit
	m.mu.Unlock()
	if a == nil {
		return 0
	}
	return a.dropped.Load()
}

// HistoryMySQL queries persisted events even after a process restart. It is
// intended only for the authenticated administrator route.
func (m *Manager) HistoryMySQL(ctx context.Context, limit int) ([]Event, []Rule, error) {
	m.mu.Lock()
	a := m.sqlAudit
	m.mu.Unlock()
	if a == nil {
		e, r := m.History(limit)
		return e, r, nil
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := a.db.QueryContext(ctx, `SELECT event_key,occurred_at,ip_address,account_mask,event_type,detail,device_json,network_kind,network_confidence,network_provider,network_source FROM risk_login_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	events := make([]Event, 0, limit)
	for rows.Next() {
		var e Event
		var raw []byte
		if err := rows.Scan(&e.ID, &e.At, &e.IP, &e.Account, &e.Type, &e.Detail, &raw, &e.Network, &e.NetworkConfidence, &e.NetworkProvider, &e.NetworkSource); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(raw, &e.Device); err != nil {
			return nil, nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	rules, err := loadSQLRules(ctx, a.db)
	return events, rules, err
}

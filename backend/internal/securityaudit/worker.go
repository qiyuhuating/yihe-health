package securityaudit

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"yihe-care/internal/db"
)

type Worker struct {
	DB         *db.Database
	URL, Token string
	Client     *http.Client
}

// Tick releases the claim transaction before external I/O. An expired worker
// cannot acknowledge a later claim, and a failed alert is retried indefinitely.
func (w *Worker) Tick(ctx context.Context) (bool, error) {
	u, err := url.Parse(w.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || w.Token == "" {
		return false, errors.New("security alert destination requires HTTPS and a token")
	}
	entry, token, err := w.claim(ctx)
	if err != nil || token == "" {
		return false, err
	}
	body, err := json.Marshal(struct {
		Priority string `json:"priority"`
		Entry
	}{Priority: "critical", Entry: entry})
	if err != nil {
		return true, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return true, err
	}
	req.Header.Set("Authorization", "Bearer "+w.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "security-"+entry.ID)
	client := *w.Client
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, sendErr := client.Do(req)
	accepted := sendErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 300
	if resp != nil {
		resp.Body.Close()
	}
	if err := w.finish(ctx, entry.ID, token, accepted); err != nil {
		return true, err
	}
	if !accepted {
		return true, errors.New("security alert not accepted; retained for retry")
	}
	return true, nil
}

func (w *Worker) claim(ctx context.Context) (Entry, string, error) {
	tx, err := w.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Entry{}, "", err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT audit_id FROM security_alert_outbox
 WHERE status IN ('pending','sending') AND available_at<=UTC_TIMESTAMP(6)
 ORDER BY available_at,audit_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, "", nil
	}
	if err != nil {
		return Entry{}, "", err
	}
	entry := Entry{ID: id}
	var before, after []byte
	err = tx.QueryRowContext(ctx, `SELECT occurred_at,os_user,os_uid,host,reason,admin_id,outcome,decision,before_state,after_state
 FROM security_audit_log WHERE id=?`, id).Scan(&entry.At, &entry.Actor.User, &entry.Actor.UID, &entry.Actor.Host,
		&entry.Reason, &entry.AdminID, &entry.Outcome, &entry.Decision, &before, &after)
	if err != nil {
		return Entry{}, "", err
	}
	if err := json.Unmarshal(before, &entry.Before); err != nil {
		return Entry{}, "", err
	}
	if err := json.Unmarshal(after, &entry.After); err != nil {
		return Entry{}, "", err
	}
	token := uuid.NewString()
	_, err = tx.ExecContext(ctx, `UPDATE security_alert_outbox SET status='sending',lease_token=?,attempts=attempts+1,
 available_at=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)) WHERE audit_id=?`, token, id)
	if err != nil {
		return Entry{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, "", err
	}
	return entry, token, nil
}

func (w *Worker) finish(ctx context.Context, id, token string, accepted bool) error {
	if accepted {
		_, err := w.DB.ExecContext(ctx, `UPDATE security_alert_outbox SET status='delivered',delivered_at=UTC_TIMESTAMP(6),last_error=''
 WHERE audit_id=? AND status='sending' AND lease_token=?`, id, token)
		return err
	}
	_, err := w.DB.ExecContext(ctx, `UPDATE security_alert_outbox SET status='pending',lease_token=NULL,last_error='delivery_not_accepted',
 available_at=TIMESTAMPADD(SECOND,LEAST(300,30*attempts),UTC_TIMESTAMP(6))
 WHERE audit_id=? AND status='sending' AND lease_token=?`, id, token)
	return err
}

func Run(ctx context.Context, store *db.Database, url, token string) {
	if url == "" || token == "" {
		slog.Warn("security alert gateway not configured; recovery alerts remain queued")
		return
	}
	w := Worker{DB: store, URL: url, Token: token, Client: &http.Client{Timeout: 5 * time.Second}}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cycle, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := w.Tick(cycle)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Warn("security alert delivery failed; retained for retry")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

package notify

import (
	"context"
	"database/sql"
	"github.com/google/uuid"

	"time"
)

func (d *Worker) claimMessages(ctx context.Context, limit int) (claimedMessageBatch, error) {
	if limit < 1 {
		return claimedMessageBatch{}, nil
	}
	limit = min(limit, 512)
	tx, err := d.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return claimedMessageBatch{}, err
	}
	defer tx.Rollback()
	condition := ""
	if deadline, ok := ctx.Deadline(); ok {
		timeout := 30 * time.Second
		if d.Client != nil && d.Client.Timeout > 0 {
			timeout = min(timeout, d.Client.Timeout)
		}
		if time.Until(deadline) <= timeout+time.Second {
			condition = " AND channel='in_app'"
		}
	}
	externalLimit := 32
	useBulk := d.bulkEnabled()
	if useBulk {
		externalLimit = 512
	}
	firstLimit := min(limit, externalLimit)
	if condition != "" {
		firstLimit = limit
	}
	selected := messageSelection{}
	err = selected.selectJobs(ctx, tx, firstLimit, condition, false, nil)
	// Fill a local-only batch without locking hundreds of external jobs that
	// cannot start before their lease expires. Typical external-only batches
	// require just the first bounded query.
	if err == nil && condition == "" && len(selected.local) > 0 && len(selected.local)+len(selected.external) < limit {
		err = selected.selectJobs(ctx, tx, limit-len(selected.local)-len(selected.external), condition, true, selected.local)
	}
	if err != nil || len(selected.local)+len(selected.external) == 0 {
		return claimedMessageBatch{}, err
	}
	token := uuid.NewString()
	for _, group := range []struct {
		jobs  []messageJob
		state string
	}{{selected.local, "delivered"}, {selected.external, "sending"}} {
		if len(group.jobs) == 0 {
			continue
		}
		ids, args := messageIDs(group.jobs, []any{token})
		query := "UPDATE message_outbox SET status='" + group.state + "',attempts=attempts+1,lease_token=?,last_attempt_at=CURRENT_TIMESTAMP(6)"
		if group.state == "delivered" {
			query += ",delivered_at=CURRENT_TIMESTAMP(6),last_error=NULL"
		}
		if _, err = tx.ExecContext(ctx, query+" WHERE id IN ("+ids+")", args...); err != nil {
			return claimedMessageBatch{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return claimedMessageBatch{}, err
	}
	return claimedMessageBatch{processed: len(selected.local) + len(selected.external), external: selected.external, token: token, useBulk: useBulk}, nil

}

type messageSelection struct{ local, external []messageJob }

func (s *messageSelection) read(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var job messageJob
		if err = rows.Scan(&job.id, &job.family, &job.topic, &job.key, &job.channel, &job.recipient, &job.attempts); err != nil {
			return err
		}
		job.attempts++
		if job.channel == "in_app" {
			s.local = append(s.local, job)
		} else {
			s.external = append(s.external, job)
		}
	}
	return rows.Err()
}

func (s *messageSelection) selectJobs(ctx context.Context, tx *sql.Tx, capacity int, condition string, onlyLocal bool, exclude []messageJob) error {
	before := len(s.local) + len(s.external)
	// Each status is an equality range with its own ordered index scan.
	// A single OR predicate requires a full scan/filesort on real MySQL.
	// Reclaimed leases and due retries run before fresh pending traffic.
	for _, phase := range []struct {
		status, column string
		cutoff         time.Time
	}{{"sending", "last_attempt_at", time.Now().Add(-time.Minute)}, {"failed", "retry_at", time.Now()}, {"pending", "retry_at", time.Now()}} {
		remaining := capacity - (len(s.local) + len(s.external) - before)
		if remaining == 0 {
			break
		}
		filter := condition
		if onlyLocal && filter == "" {
			filter = " AND channel='in_app'"
		}
		args := []any{phase.cutoff}
		if len(exclude) > 0 {
			var ids string
			ids, args = messageIDs(exclude, args)
			filter += " AND id NOT IN (" + ids + ")"
		}
		args = append(args, remaining)
		query := `SELECT id,family_id,topic,source_key,channel,recipient,attempts FROM message_outbox WHERE status='` + phase.status + `' AND ` + phase.column + `<=?` + filter + ` ORDER BY ` + phase.column + `,id LIMIT ? FOR UPDATE SKIP LOCKED`
		if err := s.read(ctx, tx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

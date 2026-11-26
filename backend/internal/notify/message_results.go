package notify

import (
	"context"
	"database/sql"
	"time"
)

// Result writes retain each job's lease token and attempts guard.
func (d *Worker) finishMessages(ctx context.Context, token string, external []messageJob, results []error) error {
	finish, err := d.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer finish.Rollback()
	groups := make(map[int][]messageJob)
	for i, job := range external {
		bucket := 0
		if results[i] != nil {
			bucket = min(job.attempts, 7)
		}
		groups[bucket] = append(groups[bucket], job)
	}
	// Consistent group order reduces lock inversions with receipt transactions.
	for attempts := 0; attempts <= 7; attempts++ {
		jobs := groups[attempts]
		if len(jobs) == 0 {
			continue
		}
		ids, fallbackArgs := messageIDs(jobs, []any{token})
		args := fallbackArgs
		set := "status='sent',last_error=NULL"
		if attempts > 0 {
			state := "failed"
			if attempts >= 5 {
				state = "exhausted"
			}
			set = "status='" + state + "',last_error='provider_not_accepted',retry_at=?"
			args = append([]any{time.Now().Add(time.Duration(30*(1<<min(attempts-1, 6))) * time.Second)}, fallbackArgs...)
		}
		if _, err = finish.ExecContext(ctx, "UPDATE message_outbox SET "+set+" WHERE lease_token=? AND status='sending' AND id IN ("+ids+")", args...); err != nil {
			return err
		}
		if attempts >= 5 {
			// The fallback INSERT binds only the lease token and selected IDs.
			if _, err = finish.ExecContext(ctx, `INSERT INTO message_outbox(topic,source_key,family_id,channel,recipient) SELECT src.topic,src.source_key,src.family_id,'voice',src.recipient FROM message_outbox AS src WHERE src.lease_token=? AND src.status='exhausted' AND src.channel='sms' AND src.id IN (`+ids+`) ON DUPLICATE KEY UPDATE message_outbox.id=message_outbox.id`, fallbackArgs...); err != nil {
				return err
			}
		}
	}
	return finish.Commit()
}

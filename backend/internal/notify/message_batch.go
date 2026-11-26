package notify

import (
	"context"
	"strings"
	"yihe-care/internal/observability"
)

type messageJob struct {
	id, family                     int64
	topic, key, channel, recipient string
	attempts                       int
}

// Drain claims a bounded batch across replicas using SKIP LOCKED. External
// requests start together so a batch never queues work beyond its lease.
func (d *Worker) Drain(ctx context.Context, limit int) (int, error) {
	batch, err := d.claimMessages(ctx, limit)
	if err != nil || batch.processed == 0 {
		return 0, err
	}
	observability.Default.Add(`yihe_notification_attempts_total{queue="message"}`, float64(batch.processed))
	if len(batch.external) == 0 {
		return batch.processed, nil
	}
	results := d.deliverMessages(ctx, batch.external, batch.useBulk)
	return batch.processed, d.finishMessages(ctx, batch.token, batch.external, results)
}

type claimedMessageBatch struct {
	processed int
	external  []messageJob
	token     string
	useBulk   bool
}

func messageIDs(jobs []messageJob, args []any) (string, []any) {
	var ids strings.Builder
	for i, job := range jobs {
		if i > 0 {
			ids.WriteByte(',')
		}
		ids.WriteByte('?')
		args = append(args, job.id)
	}
	return ids.String(), args
}

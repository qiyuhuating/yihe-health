package notify

import (
	"context"
	"sync"
	"time"
)

func (d *Worker) deliverMessages(ctx context.Context, external []messageJob, useBulk bool) []error {
	if useBulk {
		return d.deliverBulk(ctx, external)
	}
	results := make([]error, len(external))
	var workers sync.WaitGroup
	for i, job := range external {
		workers.Go(func() {
			call, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			phone, err := d.openRecipient(job.recipient)
			if err == nil {
				err = d.deliver(call, job.id, job.topic, job.key, job.channel, phone)
			}
			results[i] = err
		})
	}
	workers.Wait()
	return results
}

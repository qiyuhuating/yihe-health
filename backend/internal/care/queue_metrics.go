package care

import (
	"context"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/observability"
)

func sampleCareQueue(ctx context.Context, pool *db.Database) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var pending int
	err := pool.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT id FROM care_notification_outbox WHERE status IN ('pending','sending','failed') LIMIT 10001) active_jobs`).Scan(&pending)
	if err != nil {
		observability.Default.Set("yihe_care_queue_sample_valid", 0)
		return
	}
	var oldest *time.Time
	err = pool.QueryRowContext(ctx, `SELECT min(created) FROM (
 SELECT min(created_at) AS created FROM care_notification_outbox WHERE status='pending'
 UNION ALL SELECT min(created_at) FROM care_notification_outbox WHERE status='sending'
 UNION ALL SELECT min(created_at) FROM care_notification_outbox WHERE status='failed') active_age`).Scan(&oldest)
	if err != nil {
		observability.Default.Set("yihe_care_queue_sample_valid", 0)
		return
	}
	var age float64
	if oldest != nil {
		age = max(0, time.Since(*oldest).Seconds())
	}
	observability.Default.Set("yihe_care_queue_sample_valid", 1)
	observability.Default.Set("yihe_care_outbox_active_sample", float64(pending))
	observability.Default.Set("yihe_care_outbox_oldest_age_seconds", age)
}

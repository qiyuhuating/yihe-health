package handlers

import (
	"context"
	"log"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/health"
	"yihe-care/internal/observability"
)

// CapabilityTick performs no external I/O and is safe with concurrent instances.
func (h *Handler) CapabilityTick(ctx context.Context) error {
	var carePending, messagesPending, rulesActive int64
	if err := h.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM care_notification_outbox WHERE status IN ('pending','failed','sending') AND exhausted=false),
 (SELECT count(*) FROM message_outbox WHERE status IN ('pending','failed','sending')),(SELECT count(*) FROM rule_state WHERE active=true)`).Scan(&carePending, &messagesPending, &rulesActive); err != nil {
		return err
	}
	observability.Default.Set(`yihe_outbox_backlog{queue="care"}`, float64(carePending))
	observability.Default.Set(`yihe_outbox_backlog{queue="message"}`, float64(messagesPending))
	observability.Default.Set("yihe_rules_active", float64(rulesActive))
	observability.Default.Set("yihe_capability_heartbeat_timestamp_seconds", float64(time.Now().Unix()))
	// Binding changes invalidate pending commands; expiration never means success.
	for _, query := range [...]string{
		`UPDATE device_command SET status='cancelled' WHERE status IN ('pending','sent') AND NOT EXISTS
 (SELECT 1 FROM device d WHERE d.device_id=device_command.device_id AND d.old_id=device_command.old_id AND d.health_epoch=device_command.epoch AND d.status<>0)`,
		`UPDATE device_command SET status='timeout' WHERE status IN ('pending','sent') AND (expires_at<=CURRENT_TIMESTAMP(6) OR (status='sent' AND retry_at<=CURRENT_TIMESTAMP(6) AND attempts>=max_attempts))`,
		`UPDATE device_command SET status='pending',delivery_token=NULL WHERE status='sent' AND retry_at<=CURRENT_TIMESTAMP(6) AND expires_at>CURRENT_TIMESTAMP(6) AND attempts<max_attempts`,
	} {
		if _, err := h.DB.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	ids, err := db.QueryList(ctx, h.DB, db.ScanValue[string], "SELECT cache_key FROM metric_daily WHERE dirty=true ORDER BY updated_at LIMIT 16")
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = health.RefreshDaily(ctx, h.DB, id, h.Box); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) RunCapabilities(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := h.CapabilityTick(attempt)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Print("device capability maintenance failed; retrying")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

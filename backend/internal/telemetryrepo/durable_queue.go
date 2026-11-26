package telemetryrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/observability"
)

type DurableMessage struct {
	DeviceID     int64
	ResidentID   int64
	BindingEpoch int64
	RequestID    string
	Payload      string
	ReceivedAt   time.Time
}

func EnqueueDurable(ctx context.Context, tx *sql.Tx, message DurableMessage) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO telemetry_inbox(device_id,elder_id,binding_epoch,request_id,payload,status,created_at,next_retry_at)
		 VALUES(?,?,?,?,?,'pending',?,?)`,
		message.DeviceID, message.ResidentID, message.BindingEpoch, message.RequestID, message.Payload, message.ReceivedAt, message.ReceivedAt)
	return err
}

type DurableItem struct {
	ID           int64
	DeviceID     int64
	ResidentID   int64
	BindingEpoch int64
	Payload      string
	NextRetryAt  time.Time
	Attempts     int
}

// LockDurableBatch elects one worker and locks the oldest pending records.
// All locks remain held by the caller until its transaction commits or rolls back.
func LockDurableBatch(ctx context.Context, tx *sql.Tx, limit int) ([]DurableItem, error) {
	var guard int
	guardStarted := time.Now()
	err := tx.QueryRowContext(ctx,
		"SELECT id FROM telemetry_inbox_guard WHERE id=1 FOR UPDATE SKIP LOCKED").Scan(&guard)
	observability.Default.Add("yihe_telemetry_guard_acquire_seconds_total", time.Since(guardStarted).Seconds())
	if errors.Is(err, sql.ErrNoRows) {
		observability.Default.Add("yihe_telemetry_guard_busy_total", 1)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id,device_id,elder_id,binding_epoch,payload,next_retry_at,attempts
		 FROM telemetry_inbox WHERE status='pending'
		 ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DurableItem, 0, limit)
	for rows.Next() {
		var item DurableItem
		if err := rows.Scan(&item.ID, &item.DeviceID, &item.ResidentID,
			&item.BindingEpoch, &item.Payload, &item.NextRetryAt, &item.Attempts); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// MarkDurableFailure retains the encrypted payload and schedules bounded
// exponential retry. Permanent failures remain inspectable as dead letters.
func MarkDurableFailure(ctx context.Context, tx *sql.Tx, item DurableItem, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	next := time.Now().UTC().Add(time.Duration(1<<min(item.Attempts, 7)) * time.Second)
	status := "pending"
	if item.Attempts >= 11 {
		status = "failed"
	}
	_, err := tx.ExecContext(ctx,
		"UPDATE telemetry_inbox SET attempts=?,status=?,next_retry_at=?,last_error=? WHERE id=?",
		item.Attempts+1, status, next, reason, item.ID)
	return err
}

func MarkDurableStale(ctx context.Context, tx *sql.Tx, itemID int64, reason string) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE telemetry_inbox SET status='stale',last_error=? WHERE id=?", reason, itemID)
	return err
}

func MarkDurableDone(ctx context.Context, tx *sql.Tx, itemID int64, at time.Time) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE telemetry_inbox SET status='done',completed_at=?,last_error=NULL WHERE id=?",
		at, itemID)
	return err
}

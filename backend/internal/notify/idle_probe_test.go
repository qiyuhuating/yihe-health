package notify

import (
	"context"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

func TestIdleOutboxProbeResumesDueWork(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := t.Context()
	w := Worker{DB: p}
	now := time.Now()
	for _, status := range []string{"pending", "failed", "sending"} {
		if _, err := p.ExecContext(ctx, `INSERT INTO message_outbox
 (topic,source_key,family_id,channel,recipient,status,retry_at,last_attempt_at)
 VALUES('idle_probe',?,1,'in_app','',?,?,?)`, status, status, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
	}
	idle, err := w.drainCycle(ctx, false)
	if err != nil || !idle {
		t.Fatalf("future work must not be claimed: idle=%v err=%v", idle, err)
	}
	for _, status := range []string{"pending", "failed", "sending"} {
		idle, err = w.drainCycle(ctx, true)
		if err != nil || !idle {
			t.Fatalf("idle probe with future %s: idle=%v err=%v", status, idle, err)
		}
		if _, err = p.ExecContext(ctx, `UPDATE message_outbox SET retry_at=?,last_attempt_at=? WHERE source_key=?`, now.Add(-time.Minute), now.Add(-2*time.Minute), status); err != nil {
			t.Fatal(err)
		}
		ready, err := w.hasReadyMessages(ctx)
		if err != nil || !ready {
			t.Fatalf("due %s not found: ready=%v err=%v", status, ready, err)
		}
		idle, err = w.drainCycle(ctx, true)
		if err != nil || !idle {
			t.Fatalf("due %s not drained: idle=%v err=%v", status, idle, err)
		}
		var state string
		var attempts int
		if err = p.QueryRowContext(ctx, "SELECT status,attempts FROM message_outbox WHERE source_key=?", status).Scan(&state, &attempts); err != nil {
			t.Fatal(err)
		}
		if state != "delivered" || attempts != 1 {
			t.Fatalf("%s: status=%s attempts=%d", status, state, attempts)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if idle, err := w.drainCycle(canceled, true); err == nil || idle {
		t.Fatalf("canceled probe must not be treated as idle: idle=%v err=%v", idle, err)
	}
}

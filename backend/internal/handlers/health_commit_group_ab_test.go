package handlers

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/devicecredential"
	"yihe-care/internal/health"
)

// TestHealthDurableCommitGrouping is an isolated storage experiment, not an
// HTTP admission implementation. Independent owners are already available at
// each round; group formation, signed nonce handling and retries are excluded.
// Each group acquires device/owner locks in ascending device order and only
// acknowledges all members after the single durable commit.
func TestHealthDurableCommitGrouping(t *testing.T) {
	if os.Getenv("YIHE_HEALTH_GROUP_AB") != "1" {
		t.Skip("isolated group commit comparison")
	}
	pool := testDatabase(t)
	seedDevice(t, pool)
	const devices = 4
	const samplesPerUpload = 64
	const rounds = 128
	for i := 2; i <= devices; i++ {
		mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(?,?)", i, fmt.Sprintf("resident-%d", i))
		mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT ?,?,secret,? FROM device WHERE device_id=1", i, fmt.Sprintf("WATCH%03d", i), i)
	}
	h := testHandler(t, pool)
	ctx := t.Context()
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	type upload struct {
		device int64
		number string
		batch  health.PreparedBatch
	}
	prepare := func(version int64) (out [devices]upload) {
		t.Helper()
		for i := range out {
			id := int64(i + 1)
			number := fmt.Sprintf("WATCH%03d", id)
			samples := make([]health.Sample, samplesPerUpload)
			for k := range samples {
				samples[k] = health.Sample{RecordID: fmt.Sprintf("group-%d-%d", id, k), Source: "synthetic", SourceDevice: number, Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: version}
			}
			batch, err := health.PrepareDeviceBatch(number, samples)
			if err != nil {
				t.Fatal(err)
			}
			out[i] = upload{id, number, batch}
			out[i].batch.PrepareOwner(health.Owner{OldID: id, DeviceID: &out[i].device, Epoch: 1})
		}
		return
	}
	type result struct {
		ack, commit time.Duration
		members     int
		err         error
	}
	runGroup := func(items []upload, inject bool) (out result) {
		out.members = len(items)
		start := time.Now()
		tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			out.err = err
			return
		}
		defer tx.Rollback()
		for i := range items {
			item := &items[i]
			if inject && i == 1 {
				out.err = fmt.Errorf("injected second member failure")
				return
			}
			d, err := scanHealthDevice(tx.QueryRowContext(ctx, "SELECT "+healthDeviceCols+" FROM device WHERE device_id=? FOR UPDATE", item.device))
			if err != nil {
				out.err = err
				return
			}
			if d.No != item.number || d.ID != item.device || d.OldID == nil || *d.OldID != item.device || d.HealthEpoch != 1 || devicecredential.Check(testSecret, d.Secret, d.Status) != devicecredential.Valid {
				out.err = fmt.Errorf("binding changed")
				return
			}
			_, duplicates, err := item.batch.SaveProtected(ctx, tx, health.Owner{OldID: *d.OldID, DeviceID: &d.ID, Epoch: d.HealthEpoch}, h.Box)
			if err != nil || duplicates != 0 {
				out.err = fmt.Errorf("save: duplicates=%d err=%v", duplicates, err)
				return
			}
			if err = touchHealthDevice(ctx, tx, d.ID, time.Now()); err != nil {
				out.err = err
				return
			}
		}
		before := time.Now()
		out.err = tx.Commit()
		out.commit = time.Since(before)
		out.ack = time.Since(start)
		return
	}
	count := func(q string) int64 {
		t.Helper()
		var v int64
		if err := pool.QueryRowContext(ctx, q).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := prepare(1)
	if result := runGroup(first[:2], true); result.err == nil {
		t.Fatal("expected rollback")
	}
	if count("SELECT COUNT(*) FROM health_change") != 0 || count("SELECT COUNT(*) FROM health_sample") != 0 {
		t.Fatal("partial group escaped rollback")
	}
	var version int64
	stats := func(v []time.Duration) (float64, float64) {
		slices.Sort(v)
		return float64(v[len(v)/2]) / 1e6, float64(v[(len(v)*99+99)/100-1]) / 1e6
	}
	for _, groupSize := range []int{1, 2, 2, 1} {
		before := readRotationBinlogPosition(t, pool.DB)
		var ack, commits []time.Duration
		start := time.Now()
		for range rounds {
			version++
			uploads := prepare(version)
			var wg sync.WaitGroup
			done := make(chan result, devices/groupSize)
			barrier := make(chan struct{})
			for i := 0; i < devices; i += groupSize {
				items := uploads[i : i+groupSize]
				wg.Go(func() { <-barrier; done <- runGroup(items, false) })
			}
			close(barrier)
			wg.Wait()
			close(done)
			for r := range done {
				if r.err != nil {
					t.Fatal(r.err)
				}
				commits = append(commits, r.commit)
				for range r.members {
					ack = append(ack, r.ack)
				}
			}
		}
		elapsed := time.Since(start)
		after := readRotationBinlogPosition(t, pool.DB)
		xid := rotationXIDs(t, pool.DB, before, after)
		if xid != rounds*devices/groupSize {
			t.Fatalf("Xid=%d want %d", xid, rounds*devices/groupSize)
		}
		if count("SELECT COUNT(*) FROM health_sample") != devices*samplesPerUpload || count("SELECT COUNT(*) FROM health_change") != version*devices*samplesPerUpload {
			t.Fatal("durable rows differ from accepted versions")
		}
		ack50, ack99 := stats(ack)
		commit50, commit99 := stats(commits)
		t.Logf("GROUP size=%d logical_uploads=%d xid=%d bytes=%d samples_per_s=%.2f ack_p50_ms=%.4f ack_p99_ms=%.4f commit_p50_ms=%.4f commit_p99_ms=%.4f", groupSize, len(ack), xid, after.pos-before.pos, float64(len(ack)*samplesPerUpload)/elapsed.Seconds(), ack50, ack99, commit50, commit99)
	}
}

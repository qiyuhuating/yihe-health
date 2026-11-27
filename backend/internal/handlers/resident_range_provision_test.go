package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/health"
)

func TestResidentProvisionedRangeFirstWriteBypassesGlobalLock(t *testing.T) {
	h, admin, body := residentProfileFixture(t)
	ctx := t.Context()
	var revision, start, end, high int64
	if err := h.DB.QueryRowContext(ctx, "SELECT revision,range_start,range_end FROM health_owner_clock WHERE old_id=1").Scan(&revision, &start, &end); err != nil || revision != 0 || start != 0 || end != 65536 {
		t.Fatal("provisioned clock", revision, start, end, err)
	}
	if err := h.DB.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != end {
		t.Fatal("global range", high, err)
	}
	block, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	if err = block.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1 FOR UPDATE").Scan(&high); err != nil {
		t.Fatal(err)
	}
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	at := time.Now().UTC().Truncate(time.Microsecond)
	batch, err := health.PrepareBatch([]health.Sample{{RecordID: "first", Source: "sim", SourceDevice: "sim", Metric: "heart_rate", Unit: "count/min", Value: health.Number(70), Start: at, End: at, Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	// A failed global-lock acquisition would exceed the deadline. The new
	// owner already has a disjoint range and can publish its first event.
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cursor, _, err := batch.SaveProtected(bounded, tx, health.Owner{OldID: 1}, h.Box)
	if err != nil || cursor != 1 {
		t.Fatal("first write", cursor, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = block.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = h.DB.QueryRowContext(ctx, "SELECT revision FROM health_owner_clock WHERE old_id=1").Scan(&revision); err != nil || revision != 1 {
		t.Fatal("committed cursor", revision, err)
	}
	// Failing the owner-row update after reserving the global range must
	// roll back both the resident and the allocator.
	mustExec(t, h.DB, "CREATE TRIGGER fail_initial_range BEFORE UPDATE ON health_owner_clock FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic range failure'")
	body["name"] = "second resident"
	body["phone"] = "13800000019"
	if out := callAPI(withHealthPrincipal(h, admin, h.createResident), body); out.Code != http.StatusInternalServerError {
		t.Fatal("failed provision committed resident")
	}
	var owners, clocks int
	if err = h.DB.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM oldperson),(SELECT COUNT(*) FROM health_owner_clock),revision FROM health_clock WHERE id=1").Scan(&owners, &clocks, &high); err != nil || owners != 1 || clocks != 1 || high != end {
		t.Fatal("failed creation advanced range", owners, clocks, high, err)
	}
}

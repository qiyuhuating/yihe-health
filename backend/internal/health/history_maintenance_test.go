package health

import (
	"bytes"
	"errors"
	"testing"
)

func TestHistoryMaintenanceRoundTrip(t *testing.T) {
	p, box, samples := protectedWriteFixture(t)
	ctx := t.Context()
	if _, err := saveProtectedFixture(ctx, p, box, samples[:3]); err != nil {
		t.Fatal(err)
	}
	check := func(want int) {
		t.Helper()
		var n int
		if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample WHERE payload_cipher=0x01").Scan(&n); err != nil || n != want {
			t.Fatal(n, want, err)
		}
	}
	run := func(limit int, apply, materialize bool, want int, partial bool) {
		t.Helper()
		r, err := RewriteHistoryReferences(ctx, p.DB, box, limit, apply, materialize)
		if (partial && !errors.Is(err, ErrProjectionBackfillIncomplete)) || (!partial && err != nil) || len(r) != 1 || r[0].Verified != want {
			t.Fatal(r, err)
		}
		if apply && r[0].Migrated != want {
			t.Fatal(r)
		}
	}
	check(3)
	run(2, false, true, 2, true)
	check(3)
	run(2, true, true, 2, true)
	check(1)
	run(2, true, true, 1, false)
	check(0)
	// The independent original scope must be readable by an old reader.
	id := identityDigest(Owner{OldID: 1}, samples[0])
	var revision int64
	var cipher string
	if err := p.QueryRowContext(ctx, "SELECT revision,payload_cipher FROM health_sample WHERE sample_id=UNHEX(?)", id).Scan(&revision, &cipher); err != nil {
		t.Fatal(err)
	}
	raw, err := box.OpenBytes(cipher, samplePayloadScope(id, 1, revision))
	if err != nil {
		t.Fatal(err)
	}
	// An authenticated but different current payload must not be collapsed.
	bad := bytes.Replace(raw, []byte("heart_rate"), []byte("wrong_rate"), 1)
	if bytes.Equal(bad, raw) {
		t.Fatal("bad fixture")
	}
	badCipher, err := box.SealBytes(bad, samplePayloadScope(id, 1, revision))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE health_sample SET payload_cipher=? WHERE sample_id=UNHEX(?)", badCipher, id); err != nil {
		t.Fatal(err)
	}
	if _, err = RewriteHistoryReferences(ctx, p.DB, box, 3, true, false); err == nil {
		t.Fatal("different authenticated payload accepted")
	}
	check(0)
	if _, err = p.ExecContext(ctx, "UPDATE health_sample SET payload_cipher=? WHERE sample_id=UNHEX(?)", cipher, id); err != nil {
		t.Fatal(err)
	}
	run(3, true, false, 3, false)
	check(3)
	run(3, true, false, 0, false)
	// Missing history must fail without converting any row in the page.
	if _, err = p.ExecContext(ctx, "DELETE FROM health_change WHERE revision=?", revision); err != nil {
		t.Fatal(err)
	}
	if _, err = RewriteHistoryReferences(ctx, p.DB, box, 3, true, true); err == nil {
		t.Fatal("missing history materialized")
	}
	check(3)
}

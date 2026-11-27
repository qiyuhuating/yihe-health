package rules

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
)

func TestConsolidatedBaselineIdentityAndMigration(t *testing.T) {
	p, now := seed(t)
	ctx := t.Context()
	box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("H", 32))))
	if err != nil {
		t.Fatal(err)
	}
	ownerID := int64(1)
	owner := health.Owner{OldID: 1, DeviceID: &ownerID, Epoch: 1}
	prefix := strings.Repeat("d", 64)
	// UTF-8 bytes, not character count; the prefix can end mid-codepoint.
	wide := strings.Repeat("\u6d4b", 21) + "\u8bd5"
	devices := []string{prefix, prefix + "x", prefix + "y", wide, wide + "x", "A", "a", "A "}
	for i, device := range devices {
		for j, key := range []*securestore.Box{nil, box} {
			at := now.Add(-time.Hour * time.Duration(j+1))
			sample := health.Sample{RecordID: fmt.Sprint(i, ":", j), Source: "watch", SourceDevice: device, Metric: "heart_rate", Unit: "count/min", Start: at, End: at, Value: health.Number(float64(60 + i)), Version: 1}
			b, err := health.PrepareBatch([]health.Sample{sample})
			if err != nil {
				t.Fatal(err)
			}
			tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = b.SaveProtected(ctx, tx, owner, key); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	verify := func() {
		t.Helper()
		for i, device := range devices {
			tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			got, err := readRuleBaseline(ctx, tx, ruleDevice{id: 1, oldID: 1, epoch: 1}, "heart_rate", Value{Source: "watch", Device: device}, now, 7, box)
			tx.Rollback()
			if err != nil || got.Count != 2 || got.Mean != float64(60+i) || got.Deviation != 0 {
				t.Fatalf("device %q baseline %+v: %v", device, got, err)
			}
			var key []byte
			if err = p.QueryRowContext(ctx, "SELECT rule_identity_key FROM health_sample WHERE BINARY source_device=BINARY ? LIMIT 1", device).Scan(&key); err != nil || string(key) != string(baselineSourceKey(device)) {
				t.Fatalf("generated key %q: %x %v", device, key, err)
			}
		}
		var indexes int
		if err := p.QueryRowContext(ctx, "SELECT COUNT(DISTINCT INDEX_NAME) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='health_sample' AND INDEX_NAME IN ('idx_health_rule_source','idx_health_rule_cover')").Scan(&indexes); err != nil || indexes != 1 {
			t.Fatal("overlapping indexes", indexes, err)
		}
	}
	verify()
	for _, name := range []string{"0049_health_rule_identity_hash", "0047_health_rule_index"} {
		rollback, err := os.ReadFile("../../migrations/rollback/mysql_" + name + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(rollback), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := p.ExecContext(ctx, statement); err != nil {
				t.Fatal("rollback", err)
			}
		}
	}
	if err := db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal("reapply", err)
	}
	verify()
}

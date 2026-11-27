package health

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func TestExistingOwnerRangeProvisionMigration(t *testing.T) {
	p := dbtest.Fixture(t)
	ctx := t.Context()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := p.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO oldperson(old_id,name) VALUES(1,'existing A'),(2,'existing B'),(3,'existing C')")
	exec("UPDATE health_clock SET revision=? WHERE id=1", 2*ownerRevisionRange)
	exec("INSERT INTO health_owner_clock(old_id,revision,range_end) VALUES(1,2*65536,2*65536),(3,1,65536)")
	exec("DELETE FROM schema_migrations WHERE name='mysql/0051_existing_owner_ranges.sql'")
	if err := db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, v := range [][4]int64{{1, 2 * ownerRevisionRange, 2 * ownerRevisionRange, 3 * ownerRevisionRange}, {2, 0, 3 * ownerRevisionRange, 4 * ownerRevisionRange}, {3, 1, 0, ownerRevisionRange}} {
		var r, s, e int64
		if err := p.QueryRowContext(ctx, "SELECT revision,range_start,range_end FROM health_owner_clock WHERE old_id=?", v[0]).Scan(&r, &s, &e); err != nil || r != v[1] || s != v[2] || e != v[3] {
			t.Fatal("owner", v, r, s, e, err)
		}
	}
	var high int64
	if err := p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != 4*ownerRevisionRange {
		t.Fatal(high, err)
	}
	if err := db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal(err)
	}
	exec("DELETE FROM schema_migrations WHERE name='mysql/0051_existing_owner_ranges.sql'")
	if err := db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal("reapply", err)
	}
	if err := p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != 4*ownerRevisionRange {
		t.Fatal("reapply allocated another range", high, err)
	}
	block, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	if err = block.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1 FOR UPDATE").Scan(&high); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, v := range [][2]int64{{1, 2*ownerRevisionRange + 1}, {2, 3*ownerRevisionRange + 1}} {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
		tx, err := p.BeginTx(bounded, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		s := Sample{RecordID: "first", Source: "sim", SourceDevice: "sim", Metric: "heart_rate", Unit: "count/min", Value: Number(70), Start: at, End: at, Version: 1}
		got, _, err := SaveBatch(bounded, tx, Owner{OldID: v[0]}, []Sample{s})
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		cancel()
		if err != nil || got != v[1] {
			t.Fatal("first write", v, got, err)
		}
	}
}

package health

import (
	"database/sql"
	"strconv"
	"testing"

	"yihe-care/internal/db/dbtest"
)

func TestOwnerRangeRefillUsesSingleGlobalUpdate(t *testing.T) {
	for _, cache := range []string{"0", "32"} {
		t.Run(cache, func(t *testing.T) {
			t.Setenv("MYSQL_STMT_CACHE_SIZE", cache)
			p := dbtest.Fixture(t)
			ctx := t.Context()
			if _, err := p.ExecContext(ctx, "INSERT INTO health_owner_clock(old_id,revision,range_end) VALUES(1,0,0)"); err != nil {
				t.Fatal(err)
			}
			conn, err := p.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			selects := func() int {
				t.Helper()
				var name, value string
				if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_select'").Scan(&name, &value); err != nil {
					t.Fatal(err)
				}
				n, err := strconv.Atoi(value)
				if err != nil {
					t.Fatal(err)
				}
				return n
			}
			refill := func(start int64, commit bool) {
				t.Helper()
				tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				clock, err := lockOwner(ctx, tx, 1)
				if err != nil {
					t.Fatal(err)
				}
				got, err := clock.reserve(ctx, tx, 1, 2)
				if err != nil || got != start {
					t.Fatal("range start", got, start, err)
				}
				if commit {
					err = tx.Commit()
				} else {
					err = tx.Rollback()
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			before := selects()
			refill(0, false)
			if n := selects() - before; n != 1 {
				t.Errorf("refill SELECTs=%d want 1", n)
			}
			var high int64
			if err := p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != 0 {
				t.Fatal("rollback advanced global water mark", high, err)
			}
			if _, err := p.ExecContext(ctx, "UPDATE health_clock SET revision=? WHERE id=1", maxSafeRevision-2); err != nil {
				t.Fatal(err)
			}
			before = selects()
			refill(maxSafeRevision-2, true)
			if n := selects() - before; n != 1 {
				t.Errorf("final refill SELECTs=%d want 1", n)
			}
			if err := p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != maxSafeRevision {
				t.Fatal("safe range", high, err)
			}
		})
	}
}

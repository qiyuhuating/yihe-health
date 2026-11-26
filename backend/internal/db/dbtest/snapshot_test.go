package dbtest

import (
	"context"
	"os"
	"testing"
	"time"

	"yihe-care/internal/db"
)

func TestMySQLSnapshotIsolationAndCost(t *testing.T) {
	ctx := context.Background()
	check := func(t *testing.T, pool *db.Database) {
		t.Helper()
		var residents, revision, applied int
		if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM oldperson").Scan(&residents); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&revision); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
			t.Fatal(err)
		}
		names, err := db.MySQLMigrationNames()
		if err != nil || residents != 0 || revision != 0 || applied != len(names) {
			t.Fatalf("unexpected pristine fixture: residents=%d revision=%d migrations=%d err=%v", residents, revision, applied, err)
		}
	}

	// The standard real-MySQL gate checks the cold capture and two warm
	// restores. The dedicated experiment additionally times full migrations.
	type fixtureMode struct {
		name string
		open func(testing.TB) *db.Database
	}
	modes := []fixtureMode{{"snapshot", MySQLSnapshot}}
	if os.Getenv("TEST_MYSQL_FIXTURE_SNAPSHOT") == "1" {
		modes = append([]fixtureMode{{"fresh", MySQL}}, modes...)
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			for _, trial := range []string{"first", "second", "third"} {
				startTrial := time.Now()
				t.Run(trial, func(t *testing.T) {
					start := time.Now()
					pool := mode.open(t)
					t.Logf("fixture setup: %s", time.Since(start))
					check(t, pool)
					if mode.name == "snapshot" && trial == "third" {
						checkFixtureRejection(t, ctx, pool)
					}
					if _, err := pool.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'fixture isolation')"); err != nil {
						t.Fatal(err)
					}
					if _, err := pool.ExecContext(ctx, "UPDATE health_clock SET revision=99 WHERE id=1"); err != nil {
						t.Fatal(err)
					}
				})
				if mode.name == "snapshot" && trial == "third" {
					t.Logf("%s/%s setup, check, fault injection and cleanup: %s", mode.name, trial, time.Since(startTrial))
				} else {
					t.Logf("%s/%s setup, check and cleanup: %s", mode.name, trial, time.Since(startTrial))
				}
			}
		})
	}
}

// Reuse the final isolated fixture rather than migrating another database
// for every invalid ledger case. The validator must reject, not repair.
func checkFixtureRejection(t *testing.T, ctx context.Context, pool *db.Database) {
	t.Helper()
	names, err := db.MySQLMigrationNames()
	if err != nil {
		t.Fatal(err)
	}
	name := names[0]
	var checksum, fingerprint string
	if err = pool.QueryRowContext(ctx, "SELECT checksum,schema_checksum FROM schema_migrations WHERE name=?", name).Scan(&checksum, &fingerprint); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	reject := func() {
		t.Helper()
		if err := db.ValidateRestoredMySQLFixture(ctx, pool); err == nil {
			t.Fatal("invalid restored fixture accepted")
		}
	}
	valid := func() {
		t.Helper()
		if err := db.ValidateRestoredMySQLFixture(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE schema_migrations SET name='unknown_fixture_migration' WHERE name=?", name)
	reject()
	exec("UPDATE schema_migrations SET name=? WHERE name='unknown_fixture_migration'", name)
	exec("UPDATE schema_migrations SET checksum=REPEAT('0',64) WHERE name=?", name)
	reject()
	exec("UPDATE schema_migrations SET checksum=? WHERE name=?", checksum, name)
	exec("UPDATE schema_migrations SET schema_checksum='' WHERE name=?", name)
	reject()
	var actual string
	if err := pool.QueryRowContext(ctx, "SELECT schema_checksum FROM schema_migrations WHERE name=?", name).Scan(&actual); err != nil || actual != "" {
		t.Fatalf("validator repaired invalid ledger: %q %v", actual, err)
	}
	exec("UPDATE schema_migrations SET schema_checksum=? WHERE name=?", fingerprint, name)
	exec("INSERT INTO schema_migration_steps(name,ordinal,statement_checksum,schema_checksum) VALUES(?,0,REPEAT('0',64),'')", name)
	reject()
	exec("DELETE FROM schema_migration_steps WHERE name=?", name)
	valid()
	exec("ALTER TABLE oldperson ADD INDEX test_fixture_drift (name)")
	reject()
	exec("ALTER TABLE oldperson DROP INDEX test_fixture_drift")
	valid()
}

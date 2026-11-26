package db

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"yihe-care/migrations"
)

type migrationLedgerEntry struct {
	checksum  string
	appliedAt time.Time
}

func TestMySQLMigrationLineEndingsAndTampering(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test MySQL configuration")
	}
	admin, err := OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := "yihe_newlines_" + uuid.NewString()[:8]
	if _, err := admin.DB.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	var pool *Database
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		if _, err := admin.DB.ExecContext(context.Background(), "DROP DATABASE `"+name+"`"); err != nil {
			t.Error(err)
		}
	})
	config.DBName = name
	pool, err = OpenMySQL(ctx, config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateMySQL(ctx, pool); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.DB.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE health_clock SET revision=55 WHERE id=1")
	exec("INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic')")
	exec("INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'NEWLINE-LAB','unused',1)")
	exec("INSERT INTO care_incident(id,old_id,device_id) VALUES(1,1,1)")
	marker := strings.Repeat("1", 64)
	exec("INSERT INTO care_notification_outbox(id,incident_id,channel,recipient,recipient_key) VALUES(1,1,'sms','synthetic',?)", marker)
	capture := func() map[string]migrationLedgerEntry {
		t.Helper()
		rows, err := pool.DB.QueryContext(ctx, "SELECT name,checksum,applied_at FROM schema_migrations ORDER BY name")
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := rows.Close(); err != nil {
				t.Error(err)
			}
		}()
		result := map[string]migrationLedgerEntry{}
		for rows.Next() {
			var name string
			var value migrationLedgerEntry
			if err := rows.Scan(&name, &value.checksum, &value.appliedAt); err != nil {
				t.Fatal(err)
			}
			result[name] = value
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	original := capture()
	var names []string
	for name, value := range original {
		names = append(names, name)
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if value.checksum != digestSQL(string(data)) {
			t.Fatal("fresh ledger changed the legacy raw-byte format")
		}
	}
	sort.Strings(names)
	for _, style := range []string{"lf", "crlf", "mixed-ledger", "original-artifact"} {
		t.Run(style, func(t *testing.T) {
			for index, name := range names {
				data, err := migrations.Files.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				lf := strings.ReplaceAll(string(data), "\r\n", "\n")
				checksum := digestSQL(lf)
				if style == "crlf" || style == "mixed-ledger" && index%2 == 1 {
					checksum = digestSQL(strings.ReplaceAll(lf, "\n", "\r\n"))
				}
				if style == "original-artifact" {
					checksum = original[name].checksum
				}
				exec("UPDATE schema_migrations SET checksum=? WHERE name=?", checksum, name)
			}
			before := capture()
			if err := MigrateMySQL(ctx, pool); err != nil {
				t.Fatal("valid source newline form rejected", err)
			}
			if after := capture(); !reflect.DeepEqual(before, after) {
				t.Fatal("validation rewrote migration digests or timestamps")
			}
			var revision int64
			var recipient string
			if err := pool.DB.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&revision); err != nil || revision != 55 {
				t.Fatal("migration validation changed business revision", err)
			}
			if err := pool.DB.QueryRowContext(ctx, "SELECT recipient_key FROM care_notification_outbox WHERE id=1").Scan(&recipient); err != nil || recipient != marker {
				t.Fatal("historical migration data statements were rerun", err)
			}
		})
	}
	t.Run("unknown and changed-SQL digests fail without repairing the ledger", func(t *testing.T) {
		for _, tampered := range []string{strings.Repeat("g", 64), digestSQL("CREATE TABLE different_table (id bigint)")} {
			exec("UPDATE schema_migrations SET checksum=? WHERE name=?", tampered, names[0])
			before := capture()
			if err := MigrateMySQL(ctx, pool); err == nil || !strings.Contains(err.Error(), "migration checksum changed") {
				t.Fatal("tampered digest accepted", err)
			}
			if !reflect.DeepEqual(before, capture()) {
				t.Fatal("unrecognized ledger entry was silently repaired")
			}
		}
		exec("UPDATE schema_migrations SET checksum=? WHERE name=?", original[names[0]].checksum, names[0])
	})
	if err := MigrateMySQL(ctx, pool); err != nil {
		t.Fatal(fmt.Errorf("valid ledger did not recover after restoring its original digest: %w", err))
	}
}

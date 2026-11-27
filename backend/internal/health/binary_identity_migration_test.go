package health

import (
	"os"
	"strings"
	"testing"

	"yihe-care/internal/db"
)

func TestBinaryIdentityMigrationRoundTrip(t *testing.T) {
	p, box, samples := protectedWriteFixture(t)
	ctx := t.Context()
	if _, err := saveProtectedFixture(ctx, p, box, samples[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := Latest(ctx, p, 1, box); err != nil {
		t.Fatal(err)
	}
	samples[0].Version++
	samples[0].Value = Number(88)
	if _, err := saveProtectedFixture(ctx, p, box, samples[:1]); err != nil {
		t.Fatal(err)
	}
	type row struct {
		id, cipher string
		revision   int64
	}
	read := func(table, expr string) []row {
		t.Helper()
		rows, err := p.QueryContext(ctx, "SELECT "+expr+",payload_cipher,revision FROM "+table+" ORDER BY revision")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []row
		for rows.Next() {
			var v row
			if err := rows.Scan(&v.id, &v.cipher, &v.revision); err != nil {
				t.Fatal(err)
			}
			got = append(got, v)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return got
	}
	tables := []string{"health_sample", "health_change", "health_sample_latest"}
	before := make(map[string][]row)
	for _, table := range tables {
		before[table] = read(table, "LOWER(HEX(sample_id))")
	}
	rollback, err := os.ReadFile("../../migrations/rollback/mysql_0048_health_binary_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(rollback), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := p.ExecContext(ctx, statement); err != nil {
			t.Fatalf("rollback: %s: %v", statement, err)
		}
	}
	check := func(expr string) {
		t.Helper()
		for _, table := range tables {
			got := read(table, expr)
			if len(got) != len(before[table]) {
				t.Fatal(table, "row count changed")
			}
			for i, r := range got {
				want := before[table][i]
				if r != want {
					t.Fatal(table, "identity/revision/ciphertext changed")
				}
			}
		}
	}
	check("sample_id")
	// MigrateMySQL must accept the exact predecessor fingerprint after rollback.
	// Its own durable ledger then applies and records the conversion again.
	if err = db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal("forward after rollback", err)
	}
	check("LOWER(HEX(sample_id))")
	if err = db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal("migration replay", err)
	}
	got, err := Latest(ctx, p, 1, box)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if got[0].SampleID != identityDigest(Owner{OldID: 1}, samples[1]) {
		t.Fatal("API identity changed", got[0].SampleID)
	}
	// Replaying the same two values must still use the original authenticated
	// sample scope and version history, with no new revision.
	if duplicates, err := saveProtectedFixture(ctx, p, box, samples[:2]); err != nil || duplicates != 2 {
		t.Fatal("after migration duplicate", duplicates, err)
	}
}

package db

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"yihe-care/migrations"
)

func TestRestoredFixtureLedger(t *testing.T) {
	names, err := MySQLMigrationNames()
	if err != nil {
		t.Fatal(err)
	}
	type entry struct{ name, checksum, schema string }
	ledger := make([]entry, 0, len(names))
	for _, name := range names {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		ledger = append(ledger, entry{name, fmt.Sprintf("%x", sha256.Sum256(data)), "schema-" + name})
	}
	for _, tc := range []struct {
		name   string
		mutate func([]entry) []entry
		valid  bool
	}{
		{"complete", func(e []entry) []entry { return e }, true},
		{"missing", func(e []entry) []entry { return e[1:] }, false},
		{"unexpected", func(e []entry) []entry { e[0].name = "unknown"; return e }, false},
		{"checksum", func(e []entry) []entry { e[0].checksum = "invalid"; return e }, false},
		{"fingerprint", func(e []entry) []entry { e[len(e)-1].schema = ""; return e }, false},
		{"extra", func(e []entry) []entry { return append(e, entry{"extra", "invalid", "invalid"}) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := tc.mutate(slices.Clone(ledger))
			i := 0
			last, err := validateFixtureLedger(names, func() (string, string, string, bool, error) {
				if i == len(entries) {
					return "", "", "", false, nil
				}
				row := entries[i]
				i++
				return row.name, row.checksum, row.schema, true, nil
			})
			if tc.valid {
				if err != nil || last != ledger[len(ledger)-1].schema {
					t.Fatalf("complete ledger rejected: %s %v", last, err)
				}
			} else if err == nil {
				t.Fatal("invalid ledger accepted")
			}
		})
	}
}

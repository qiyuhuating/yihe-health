package db

import "testing"

func TestMigrationStatementKind(t *testing.T) {
	for _, tc := range []struct{ statement, kind string }{
		{"-- comment\nCREATE TABLE IF NOT EXISTS records (id BIGINT)", "CREATE"},
		{"# comment\nALTER TABLE records ADD COLUMN name VARCHAR(32)", "ALTER"},
		{"INSERT INTO records(id) VALUES(1)", "INSERT"},
		{"UPDATE records SET id=2 WHERE id=1", "UPDATE"},
		{"-- comment only", ""},
	} {
		if got := migrationStatementKind(tc.statement); got != tc.kind {
			t.Errorf("kind of %q = %q; want %q", tc.statement, got, tc.kind)
		}
	}
}

func TestMigrationStatementChecksumRejectsChangedSQL(t *testing.T) {
	if migrationStatementDigest("ALTER TABLE records ADD COLUMN age INT") ==
		migrationStatementDigest("ALTER TABLE records ADD COLUMN age BIGINT") {
		t.Fatal("changed SQL must not match an earlier checkpoint")
	}
}

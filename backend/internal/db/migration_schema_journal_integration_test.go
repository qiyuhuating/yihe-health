package db

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"yihe-care/migrations"
)

func journalDatabase(t *testing.T) (*Database, *sql.Conn) {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	name := "yihe_journal_" + uuid.NewString()[:8]
	if _, err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	pool, err := OpenMySQL(context.Background(), cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		pool.Close()
		if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	return pool, conn
}

func journalExec(t *testing.T, conn *sql.Conn, query string, args ...any) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLMigrationRejectsUnknownHistoryBeforeDDL(t *testing.T) {
	for _, tc := range []struct{ table, name string }{
		{"schema_migrations", "mysql/0033_social_phone_protection.sql"},
		{"schema_migration_steps", "mysql/0034_resident_care_profile_protection.sql"},
		{"schema_migration_steps", "mysql/0035_guardian_contact_protection.sql"},
		{"schema_migrations", "mysql/9999_future.sql"},
	} {
		t.Run(tc.table+"/"+tc.name, func(t *testing.T) {
			pool, conn := journalDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			// An old ledger lacks schema_checksum. Rejection must precede even
			// adding that column or creating the missing step journal.
			journalExec(t, conn, "CREATE TABLE schema_migrations(name VARCHAR(191) PRIMARY KEY,checksum CHAR(64) NOT NULL) ENGINE=InnoDB")
			journalExec(t, conn, "CREATE TABLE records(id INT PRIMARY KEY,n INT) ENGINE=InnoDB")
			journalExec(t, conn, "INSERT INTO records VALUES(1,47)")
			if tc.table == "schema_migrations" {
				journalExec(t, conn, "INSERT INTO schema_migrations VALUES(?,?)", tc.name, strings.Repeat("1", 64))
			} else {
				if err := ensureMigrationStepJournal(ctx, conn); err != nil {
					t.Fatal(err)
				}
				journalExec(t, conn, "INSERT INTO schema_migration_steps VALUES(?,?,?,?)", tc.name, 0, strings.Repeat("2", 64), "")
			}
			before, err := schemaDigest(ctx, conn)
			if err != nil {
				t.Fatal(err)
			}
			if err := MigrateMySQL(ctx, pool); err == nil || !strings.Contains(err.Error(), "unknown MySQL migration in "+tc.table+": "+tc.name) {
				t.Fatalf("unknown history not rejected before migration: %v", err)
			}
			after, err := schemaDigest(ctx, conn)
			if err != nil || before != after {
				t.Fatalf("business schema changed on rejected history: %v", err)
			}
			var value, fingerprintColumns, journals int
			if err := conn.QueryRowContext(ctx, "SELECT n FROM records WHERE id=1").Scan(&value); err != nil || value != 47 {
				t.Fatalf("business data changed: %d %v", value, err)
			}
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migrations' AND COLUMN_NAME='schema_checksum'").Scan(&fingerprintColumns); err != nil || fingerprintColumns != 0 {
				t.Fatalf("ledger DDL executed before rejection: %d %v", fingerprintColumns, err)
			}
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migration_steps'").Scan(&journals); err != nil || (journals == 1) != (tc.table == "schema_migration_steps") {
				t.Fatalf("step journal changed: %d %v", journals, err)
			}
			var count int
			if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tc.table+" WHERE name=?", tc.name).Scan(&count); err != nil || count != 1 {
				t.Fatalf("unknown history was rewritten: %d %v", count, err)
			}
		})
	}
}

func TestMySQLJournalInterruptedDDL(t *testing.T) {
	for _, applied := range []bool{false, true} {
		name := "intent only"
		if applied {
			name = "DDL committed before checkpoint"
		}
		t.Run(name, func(t *testing.T) {
			_, conn := journalDatabase(t)
			ctx := context.Background()
			if err := ensureMigrationStepJournal(ctx, conn); err != nil {
				t.Fatal(err)
			}
			statement := "CREATE TABLE records (id INT) ENGINE=InnoDB"
			journalExec(t, conn, "INSERT INTO schema_migration_steps VALUES(?,?,?,?)", "test", 0, migrationStatementDigest(statement), "")
			if applied {
				journalExec(t, conn, statement)
			}
			if _, err := resumeMigrationAt(ctx, conn, "test", []string{statement}); err == nil || !strings.Contains(err.Error(), "interrupted DDL") {
				t.Fatalf("unknown DDL accepted: %v", err)
			}
		})
	}
}

func TestMySQLJournalDMLAtomicity(t *testing.T) {
	_, conn := journalDatabase(t)
	ctx := context.Background()
	if err := ensureMigrationStepJournal(ctx, conn); err != nil {
		t.Fatal(err)
	}
	journalExec(t, conn, "CREATE TABLE records (id INT PRIMARY KEY, n INT) ENGINE=InnoDB")
	journalExec(t, conn, "INSERT INTO records VALUES(1,0)")
	statement := "UPDATE records SET n=n+1 WHERE id=1"
	if err := applyCheckpointedStatement(ctx, conn, "test", 0, statement); err != nil {
		t.Fatal(err)
	}
	next, err := resumeMigrationAt(ctx, conn, "test", []string{statement})
	if err != nil || next != 1 {
		t.Fatalf("committed DML/checkpoint replayed: %d %v", next, err)
	}
	// A duplicate checkpoint fails after UPDATE and must roll the UPDATE back.
	if err := applyCheckpointedStatement(ctx, conn, "test", 0, statement); err == nil {
		t.Fatal("duplicate checkpoint accepted")
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT n FROM records WHERE id=1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("DML escaped rollback: %d %v", n, err)
	}
}

func TestMySQLJournalRejectsSchemaDrift(t *testing.T) {
	for _, tc := range []struct{ name, change string }{
		{"column type", "ALTER TABLE child MODIFY a BIGINT"},
		{"index order", "ALTER TABLE child DROP INDEX idx, ADD INDEX idx(b,a)"},
		{"uniqueness", "ALTER TABLE child DROP INDEX idx, ADD UNIQUE INDEX idx(a,b)"},
		{"index prefix", "ALTER TABLE child DROP INDEX txt, ADD INDEX txt(label(12))"},
		{"foreign reference", "ALTER TABLE child ADD CONSTRAINT fk FOREIGN KEY(p) REFERENCES parent(other)"},
		{"check enforcement", "ALTER TABLE child ALTER CHECK positive NOT ENFORCED"},
		{"generated expression", "ALTER TABLE child MODIFY g INT GENERATED ALWAYS AS (a+2) STORED"},
		{"trigger", "DROP TRIGGER child_insert"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, conn := journalDatabase(t)
			ctx := context.Background()
			if err := ensureMigrationStepJournal(ctx, conn); err != nil {
				t.Fatal(err)
			}
			journalExec(t, conn, "CREATE TABLE parent(id INT PRIMARY KEY,other INT UNIQUE) ENGINE=InnoDB")
			journalExec(t, conn, "CREATE TABLE child(a INT,b INT,p INT,label VARCHAR(64),g INT GENERATED ALWAYS AS(a+1) STORED,INDEX idx(a,b),INDEX txt(label(8)),CONSTRAINT fk FOREIGN KEY(p) REFERENCES parent(id),CONSTRAINT positive CHECK(a>0)) ENGINE=InnoDB")
			journalExec(t, conn, "CREATE TRIGGER child_insert BEFORE INSERT ON child FOR EACH ROW SET NEW.b=1")
			statement := "UPDATE child SET b=2 WHERE a=1"
			if err := applyCheckpointedStatement(ctx, conn, "test", 0, statement); err != nil {
				t.Fatal(err)
			}
			if tc.name == "foreign reference" {
				journalExec(t, conn, "ALTER TABLE child DROP FOREIGN KEY fk")
			}
			journalExec(t, conn, tc.change)
			if tc.name == "trigger" {
				journalExec(t, conn, "CREATE TRIGGER child_insert BEFORE INSERT ON child FOR EACH ROW SET NEW.b=3")
			}
			if _, err := resumeMigrationAt(ctx, conn, "test", []string{statement}); err == nil {
				t.Fatal("schema drift accepted")
			}
			var completedSchema string
			if err := conn.QueryRowContext(ctx, "SELECT schema_checksum FROM schema_migration_steps WHERE name='test'").Scan(&completedSchema); err != nil {
				t.Fatal(err)
			}
			journalExec(t, conn, "CREATE TABLE schema_migrations(name VARCHAR(191) PRIMARY KEY,schema_checksum CHAR(64) NOT NULL) ENGINE=InnoDB")
			journalExec(t, conn, "INSERT INTO schema_migrations VALUES(?,?)", "test", completedSchema)
			if err := validateCompletedMigrationSchema(ctx, conn, []string{"test"}); err == nil {
				t.Fatal("completed ledger accepted schema drift")
			}
		})
	}
}

func TestMySQLJournalRejectsExistingTable(t *testing.T) {
	pool, conn := journalDatabase(t)
	if err := ensureMigrationStepJournal(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	journalExec(t, conn, "CREATE TABLE records(id VARCHAR(20))")
	if err := applyCheckpointedStatement(context.Background(), conn, "test", 0, "CREATE TABLE IF NOT EXISTS records (id INT)"); err == nil {
		t.Fatal("IF NOT EXISTS hid incompatible structure")
	}
	journalExec(t, conn, "CREATE TABLE schema_migrations(name VARCHAR(191) PRIMARY KEY,checksum CHAR(64) NOT NULL,applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)) ENGINE=InnoDB")
	journalExec(t, conn, "CREATE TABLE alarm_precess(id VARCHAR(20))")
	if err := MigrateMySQL(context.Background(), pool); err == nil {
		t.Fatal("full migrator accepted incompatible table")
	}
	var recorded int
	if err := conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("failed migration recorded success: %d %v", recorded, err)
	}
}

func TestMySQLCompletedSchemaRejectsDrift(t *testing.T) {
	pool, conn := journalDatabase(t)
	ctx := context.Background()
	if err := MigrateMySQL(ctx, pool); err != nil {
		t.Fatal(err)
	}
	journalExec(t, conn, "ALTER TABLE users MODIFY user_name VARCHAR(99)")
	if err := MigrateMySQL(ctx, pool); err == nil {
		t.Fatal("completed migration ledger hid a changed column type")
	}
}

func TestMySQLJournalRejectsNontransactionalMetadata(t *testing.T) {
	for _, table := range []string{"schema_migrations", "schema_migration_steps"} {
		t.Run(table, func(t *testing.T) {
			pool, conn := journalDatabase(t)
			ledgerEngine, journalEngine := "InnoDB", "InnoDB"
			if table == "schema_migrations" {
				ledgerEngine = "MyISAM"
			} else {
				journalEngine = "MyISAM"
			}
			journalExec(t, conn, "CREATE TABLE schema_migrations(name VARCHAR(191) PRIMARY KEY,checksum CHAR(64) NOT NULL,applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)) ENGINE="+ledgerEngine)
			journalExec(t, conn, "CREATE TABLE schema_migration_steps(name VARCHAR(191),ordinal INT,statement_checksum CHAR(64),schema_checksum CHAR(64),PRIMARY KEY(name,ordinal)) ENGINE="+journalEngine)
			if err := MigrateMySQL(context.Background(), pool); err == nil || !strings.Contains(err.Error(), "requires InnoDB") {
				t.Fatalf("nontransactional metadata accepted: %v", err)
			}
		})
	}
}

func TestMySQLJournalFreshRepeatAndLegacyUpgrade(t *testing.T) {
	pool, conn := journalDatabase(t)
	ctx := context.Background()
	if err := MigrateMySQL(ctx, pool); err != nil {
		t.Fatal(err)
	}
	first, err := schemaDigest(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateMySQL(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// Crash after the final checkpoint but before the migration ledger commit.
	names, err := MySQLMigrationNames()
	if err != nil {
		t.Fatal(err)
	}
	last := names[len(names)-1]
	data, err := migrations.Files.ReadFile(last)
	if err != nil {
		t.Fatal(err)
	}
	ordinal := 0
	for _, statement := range strings.Split(string(data), ";") {
		if migrationStatementKind(statement) == "" {
			continue
		}
		journalExec(t, conn, "INSERT INTO schema_migration_steps VALUES(?,?,?,?)", last, ordinal, migrationStatementDigest(statement), first)
		ordinal++
	}
	journalExec(t, conn, "DELETE FROM schema_migrations WHERE name=?", last)
	if err := MigrateMySQL(ctx, pool); err != nil {
		t.Fatal("final checkpoint recovery:", err)
	}
	second, err := schemaDigest(ctx, conn)
	if err != nil || first != second {
		t.Fatalf("repeat changed schema: %v", err)
	}
	// Rebuild the historical pre-journal schema directly from embedded SQL.
	legacy, legacyConn := journalDatabase(t)
	journalExec(t, legacyConn, "CREATE TABLE schema_migrations(name VARCHAR(191) PRIMARY KEY,checksum CHAR(64) NOT NULL,applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)) ENGINE=InnoDB")
	for _, name := range names {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(data), ";") {
			if migrationStatementKind(statement) != "" {
				journalExec(t, legacyConn, statement)
			}
		}
		journalExec(t, legacyConn, "INSERT INTO schema_migrations(name,checksum) VALUES(?,?)", name, digestSQL(string(data)))
	}
	expected, err := schemaDigest(ctx, legacyConn)
	if err != nil || expected != first {
		t.Fatalf("journal changed full schema inventory: %v", err)
	}
	if err := MigrateMySQL(ctx, legacy); err != nil {
		t.Fatal("legacy upgrade:", err)
	}
	upgraded, err := schemaDigest(ctx, legacyConn)
	if err != nil || upgraded != expected {
		t.Fatalf("legacy upgrade changed schema: %v", err)
	}
}

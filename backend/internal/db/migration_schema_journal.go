package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Schema fingerprints deliberately exclude row counts and volatile statistics.
// Changes to columns, constraints, indexes and triggers invalidate checkpoints.
var migrationSchemaQueries = []string{
	`SELECT TABLE_NAME,TABLE_TYPE,COALESCE(ENGINE,''),COALESCE(TABLE_COLLATION,'') FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME NOT IN ('schema_migrations','schema_migration_steps') ORDER BY TABLE_NAME`,
	`SELECT TABLE_NAME,ORDINAL_POSITION,COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT,EXTRA,COALESCE(GENERATION_EXPRESSION,''),COALESCE(CHARACTER_SET_NAME,''),COALESCE(COLLATION_NAME,'') FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME NOT IN ('schema_migrations','schema_migration_steps') ORDER BY TABLE_NAME,ORDINAL_POSITION`,
	`SELECT TABLE_NAME,INDEX_NAME,NON_UNIQUE,SEQ_IN_INDEX,COALESCE(COLUMN_NAME,''),COALESCE(SUB_PART,0),INDEX_TYPE,COALESCE(EXPRESSION,''),COLLATION,IS_VISIBLE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME NOT IN ('schema_migrations','schema_migration_steps') ORDER BY TABLE_NAME,INDEX_NAME,SEQ_IN_INDEX`,
	`SELECT TABLE_NAME,CONSTRAINT_NAME,CONSTRAINT_TYPE,ENFORCED FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME NOT IN ('schema_migrations','schema_migration_steps') ORDER BY TABLE_NAME,CONSTRAINT_NAME`,
	`SELECT TABLE_NAME,CONSTRAINT_NAME,ORDINAL_POSITION,COLUMN_NAME,COALESCE(REFERENCED_TABLE_NAME,''),COALESCE(REFERENCED_COLUMN_NAME,''),CASE WHEN REFERENCED_TABLE_SCHEMA=DATABASE() THEN '' ELSE COALESCE(REFERENCED_TABLE_SCHEMA,'') END FROM information_schema.KEY_COLUMN_USAGE WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME NOT IN ('schema_migrations','schema_migration_steps') ORDER BY TABLE_NAME,CONSTRAINT_NAME,ORDINAL_POSITION`,
	`SELECT TABLE_NAME,CONSTRAINT_NAME,UPDATE_RULE,DELETE_RULE,REFERENCED_TABLE_NAME FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE() ORDER BY TABLE_NAME,CONSTRAINT_NAME`,
	`SELECT tc.TABLE_NAME,cc.CONSTRAINT_NAME,cc.CHECK_CLAUSE FROM information_schema.CHECK_CONSTRAINTS cc JOIN information_schema.TABLE_CONSTRAINTS tc ON tc.CONSTRAINT_SCHEMA=cc.CONSTRAINT_SCHEMA AND tc.CONSTRAINT_NAME=cc.CONSTRAINT_NAME WHERE cc.CONSTRAINT_SCHEMA=DATABASE() ORDER BY tc.TABLE_NAME,cc.CONSTRAINT_NAME`,
	`SELECT TRIGGER_NAME,EVENT_OBJECT_TABLE,ACTION_TIMING,EVENT_MANIPULATION,ACTION_STATEMENT,ACTION_ORIENTATION,SQL_MODE FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() ORDER BY TRIGGER_NAME`,
}

func schemaDigest(ctx context.Context, conn *sql.Conn) (string, error) {
	hash := sha256.New()
	enc := json.NewEncoder(hash)
	for n, query := range migrationSchemaQueries {
		rows, err := conn.QueryContext(ctx, query)
		if err != nil {
			return "", fmt.Errorf("schema inventory %d: %w", n, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return "", err
		}
		if err = enc.Encode(n); err != nil {
			rows.Close()
			return "", err
		}
		record := newSchemaRecord(len(columns))
		for rows.Next() {
			if err = rows.Scan(record.dest...); err != nil {
				break
			}
			if err = enc.Encode(record.values()); err != nil {
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Allocate scan destinations and nullable string slots once per result set.
// Copy RawBytes before the next scan; encoding retains the existing JSON
// byte format (including null, empty strings and HTML escaping).
type schemaRecord struct {
	raw    []sql.RawBytes
	dest   []any
	text   []string
	fields []*string
}

func newSchemaRecord(n int) *schemaRecord {
	r := &schemaRecord{raw: make([]sql.RawBytes, n), dest: make([]any, n), text: make([]string, n), fields: make([]*string, n)}
	for i := range r.raw {
		r.dest[i] = &r.raw[i]
	}
	return r
}

func (r *schemaRecord) values() *[]*string {
	for i, v := range r.raw {
		if v == nil {
			r.text[i] = ""
			r.fields[i] = nil
			continue
		}
		r.text[i] = string(v)
		r.fields[i] = &r.text[i]
	}
	return &r.fields
}

func ensureMigrationStepJournal(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migration_steps (
  name VARCHAR(191) NOT NULL, ordinal INT NOT NULL,
  statement_checksum CHAR(64) NOT NULL, schema_checksum CHAR(64) NOT NULL,
  PRIMARY KEY(name,ordinal)) ENGINE=InnoDB`)
	return err
}

func migrationStatementDigest(statement string) string {
	sum := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(sum[:])
}

// Known ledger holes may be deliberate rollbacks. Unknown completed migrations
// or statement intents instead require a matching release/backup and inspection.
func validateMigrationHistoryNames(ctx context.Context, conn *sql.Conn, names []string) error {
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}
	for _, table := range []string{"schema_migrations", "schema_migration_steps"} {
		var exists int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?", table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		// table is selected exclusively from the fixed internal list above.
		rows, err := conn.QueryContext(ctx, "SELECT DISTINCT name FROM "+table+" ORDER BY name")
		if err != nil {
			return err
		}
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				break
			}
			if !known[name] {
				err = fmt.Errorf("unknown MySQL migration in %s: %s; use a matching release or restore a matching backup before migration; manual inspection required", table, name)
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// Validate a completed prefix before extending it or accepting a no-op run.
// Partial migration recovery uses its statement checkpoints instead. A ledger
// with holes represents explicit rollback and must be reapplied before a new
// completed fingerprint can be recorded.
func validateCompletedMigrationSchema(ctx context.Context, conn *sql.Conn, names []string) error {
	var pending int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migration_steps s
 LEFT JOIN schema_migrations m ON m.name=s.name WHERE m.name IS NULL`).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return nil
	}
	rows, err := conn.QueryContext(ctx, "SELECT name,schema_checksum FROM schema_migrations ORDER BY name")
	if err != nil {
		return err
	}
	next, prefix := 0, true
	var lastName, lastChecksum string
	for rows.Next() {
		if err = rows.Scan(&lastName, &lastChecksum); err != nil {
			break
		}
		if next >= len(names) || lastName != names[next] {
			prefix = false
		}
		next++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if !prefix || lastChecksum == "" {
		return nil
	}
	actual, err := schemaDigest(ctx, conn)
	if err != nil {
		return err
	}
	if actual != lastChecksum {
		return fmt.Errorf("MySQL schema differs from completed migration %s; manual inspection required", lastName)
	}
	return nil
}

func migrationStatementKind(text string) string {
	for {
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "--") || strings.HasPrefix(text, "#") {
			end := strings.IndexByte(text, '\n')
			if end < 0 {
				return ""
			}
			text = text[end+1:]
			continue
		}
		break
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

// A checkpoint validates the entire schema as it was after the last
// committed statement; a change made between DDL and checkpoint is rejected.
func resumeMigrationAt(ctx context.Context, conn *sql.Conn, name string, statements []string) (int, error) {
	rows, err := conn.QueryContext(ctx, `SELECT ordinal,statement_checksum,schema_checksum FROM schema_migration_steps WHERE name=? ORDER BY ordinal`, name)
	if err != nil {
		return 0, err
	}
	next, lastSchema := 0, ""
	for rows.Next() {
		var ordinal int
		var statementHash, schemaHash string
		if err = rows.Scan(&ordinal, &statementHash, &schemaHash); err != nil {
			break
		}
		if ordinal != next || ordinal >= len(statements) || statementHash != migrationStatementDigest(statements[ordinal]) {
			err = fmt.Errorf("migration %s checkpoint mismatch at step %d", name, ordinal)
			break
		}
		lastSchema = schemaHash
		if schemaHash == "" {
			err = fmt.Errorf("migration %s has an interrupted DDL at step %d; manual inspection required", name, ordinal)
			break
		}
		next++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, err
	}
	if next != 0 {
		actual, err := schemaDigest(ctx, conn)
		if err != nil {
			return 0, err
		}
		if actual != lastSchema {
			return 0, fmt.Errorf("migration %s schema differs from last committed checkpoint; manual inspection required", name)
		}
	}
	return next, nil
}

// DML and its checkpoint commit in one transaction; MySQL DDL cannot be made
// atomic with its checkpoint, so unknown mid-DDL state must fail closed.
func applyCheckpointedStatement(ctx context.Context, conn *sql.Conn, name string, ordinal int, statement string) error {
	kind := migrationStatementKind(statement)
	checksum := migrationStatementDigest(statement)
	insert := `INSERT INTO schema_migration_steps(name,ordinal,statement_checksum,schema_checksum) VALUES(?,?,?,?)`
	switch kind {
	case "INSERT", "UPDATE", "DELETE", "REPLACE":
		before, err := schemaDigest(ctx, conn)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, insert, name, ordinal, checksum, before); err != nil {
			return err
		}
		return tx.Commit()
	case "CREATE", "ALTER", "DROP", "RENAME":
		if err := rejectUntrackedTable(ctx, conn, statement); err != nil {
			return err
		}
		// MySQL implicitly commits DDL. Persist intent first so even a first-step
		// crash cannot replay a destructive or data-changing DDL statement.
		if _, err := conn.ExecContext(ctx, insert, name, ordinal, checksum, ""); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
		after, err := schemaDigest(ctx, conn)
		if err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx, `UPDATE schema_migration_steps SET schema_checksum=? WHERE name=? AND ordinal=?`, after, name, ordinal)
		return err
	default:
		return fmt.Errorf("unsupported migration statement kind %q in %s", kind, name)
	}
}

func rejectUntrackedTable(ctx context.Context, conn *sql.Conn, statement string) error {
	// Migrations commonly place comments before CREATE TABLE.
	for {
		statement = strings.TrimSpace(statement)
		if strings.HasPrefix(statement, "--") || strings.HasPrefix(statement, "#") {
			newline := strings.IndexByte(statement, '\n')
			if newline < 0 {
				return nil
			}
			statement = statement[newline+1:]
			continue
		}
		break
	}
	fields := strings.Fields(statement)
	if len(fields) < 6 || !strings.EqualFold(fields[0], "CREATE") || !strings.EqualFold(fields[1], "TABLE") ||
		!strings.EqualFold(fields[2], "IF") || !strings.EqualFold(fields[3], "NOT") || !strings.EqualFold(fields[4], "EXISTS") {
		return nil
	}
	name := strings.Trim(fields[5], "` ")
	if strings.ContainsAny(name, ".();") {
		return errors.New("unsupported CREATE TABLE identifier in migration")
	}
	var exists int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, name).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return fmt.Errorf("untracked table %q already exists; cannot establish migration equivalence", name)
	}
	return nil
}

package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"yihe-care/migrations"
)

// MySQLMigrationNames returns the embedded MySQL migrations in dependency
// order. The embedded filesystem is the single source of truth: adding a
// migration file is enough, with no registry or count to update elsewhere.
func MySQLMigrationNames() ([]string, error) {
	names, err := fs.Glob(migrations.Files, "mysql/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded MySQL migrations: %w", err)
	}
	if len(names) == 0 {
		return nil, errors.New("no embedded MySQL migrations")
	}
	sort.Strings(names)
	return names, nil
}

// MySQL DDL commits implicitly. A connection-scoped lock serializes migrations;
// statement checkpoints prevent replay of ambiguous DDL or committed DML.
func MigrateMySQL(ctx context.Context, database *Database) (result error) {
	conn, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	discard := func() {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	var locked sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('yihe-migrate:',DATABASE()),30)").Scan(&locked); err != nil {
		discard()
		return fmt.Errorf("acquire MySQL migration lock: %w", err)
	}
	if !locked.Valid || locked.Int64 != 1 {
		return errors.New("MySQL migration lock unavailable")
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released sql.NullInt64
		err := conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(CONCAT('yihe-migrate:',DATABASE()))").Scan(&released)
		if err == nil && released.Valid && released.Int64 == 1 {
			return
		}
		discard()
		if err == nil {
			err = errors.New("server did not confirm lock release")
		}
		result = errors.Join(result, fmt.Errorf("release MySQL migration lock: %w", err))
	}()
	names, err := MySQLMigrationNames()
	if err != nil {
		return err
	}
	// Reject foreign or newer histories before even changing ledger metadata.
	// MySQL DDL cannot be rolled back after discovering an incompatible name.
	if err := validateMigrationHistoryNames(ctx, conn, names); err != nil {
		return err
	}
	var known, tables int
	if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='schema_migrations'").Scan(&known); err != nil {
		return err
	}
	if known == 0 {
		if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables); err != nil {
			return err
		}
		if tables != 0 {
			return errors.New("refusing to initialize a nonempty unrecognized MySQL database")
		}
	}
	// Statement effects, checkpoints and the completed ledger must all be
	// transactional. A pre-existing non-InnoDB table breaks that guarantee.
	var nonTransactional string
	err = conn.QueryRowContext(ctx, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE' AND ENGINE<>'InnoDB' LIMIT 1").Scan(&nonTransactional)
	if err == nil {
		return fmt.Errorf("MySQL migration requires InnoDB table %s", nonTransactional)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(name varchar(191) PRIMARY KEY,checksum char(64) NOT NULL,schema_checksum char(64) NOT NULL DEFAULT '',applied_at datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)) ENGINE=InnoDB"); err != nil {
		return err
	}
	var fingerprintColumn int
	if err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='schema_migrations' AND COLUMN_NAME='schema_checksum'").Scan(&fingerprintColumn); err != nil {
		return err
	}
	if fingerprintColumn == 0 {
		if _, err = conn.ExecContext(ctx, "ALTER TABLE schema_migrations ADD COLUMN schema_checksum CHAR(64) NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if err = ensureMigrationStepJournal(ctx, conn); err != nil {
		return fmt.Errorf("initialize migration step journal: %w", err)
	}
	if err := validateCompletedMigrationSchema(ctx, conn, names); err != nil {
		return err
	}
	for _, name := range names {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			return err
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(data))
		var applied string
		err = conn.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE name=?", name).Scan(&applied)
		if err == nil {
			matches, checkErr := migrationChecksumMatches(data, applied)
			if checkErr != nil {
				return fmt.Errorf("cannot validate MySQL migration SQL: %s: %w", name, checkErr)
			}
			if !matches {
				return fmt.Errorf("migration checksum changed: %s", name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// MySQL DDL commits implicitly. Resume only after checking committed step
		// fingerprints; never classify an unknown duplicate object as success.
		statements := make([]string, 0)
		for _, statement := range strings.Split(string(data), ";") {
			if migrationStatementKind(statement) != "" {
				statements = append(statements, statement)
			}
		}
		start, err := resumeMigrationAt(ctx, conn, name, statements)
		if err != nil {
			return err
		}
		for ordinal := start; ordinal < len(statements); ordinal++ {
			if err := applyCheckpointedStatement(ctx, conn, name, ordinal, statements[ordinal]); err != nil {
				return fmt.Errorf("MySQL migration %s step %d: %w", name, ordinal, err)
			}
		}
		// Retire statement checkpoints atomically with the completed ledger entry.
		// Otherwise an intentional rollback leaves stale fingerprints that reject
		// reapplication, and later migrations invalidate those fingerprints.
		schemaChecksum, err := schemaDigest(ctx, conn)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(name,checksum,schema_checksum) VALUES(?,?,?)", name, sum, schemaChecksum); err == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM schema_migration_steps WHERE name=?", name)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	// Older completed ledgers have no structural fingerprint. Establish their
	// baseline only after every historical SQL checksum has been validated.
	schemaChecksum, err := schemaDigest(ctx, conn)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "UPDATE schema_migrations SET schema_checksum=? WHERE name=? AND schema_checksum=''", schemaChecksum, names[len(names)-1])
	return err
}

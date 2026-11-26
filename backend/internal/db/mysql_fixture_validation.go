package db

import (
	"context"
	"fmt"

	"yihe-care/migrations"
)

// ValidateRestoredMySQLFixture verifies a freshly restored, exclusively owned
// disposable test schema before publishing it to any test. Unlike MigrateMySQL,
// it cannot repair an incomplete ledger or execute DDL/DML. Do not use this in
// place of the production migrator or for a schema with concurrent writers.
func ValidateRestoredMySQLFixture(ctx context.Context, database *Database) error {
	names, err := MySQLMigrationNames()
	if err != nil {
		return err
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var pending, ledgerTables int
	err = conn.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM schema_migration_steps),
 (SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()
 AND TABLE_NAME IN ('schema_migrations','schema_migration_steps')
 AND TABLE_TYPE='BASE TABLE' AND ENGINE='InnoDB')`).Scan(&pending, &ledgerTables)
	if err != nil {
		return err
	}
	if pending != 0 || ledgerTables != 2 {
		return fmt.Errorf("invalid fixture migration journal: pending=%d ledger_tables=%d", pending, ledgerTables)
	}

	rows, err := conn.QueryContext(ctx, "SELECT name,checksum,schema_checksum FROM schema_migrations ORDER BY name")
	if err != nil {
		return err
	}
	last, err := validateFixtureLedger(names, func() (name, checksum, schema string, ok bool, err error) {
		if !rows.Next() {
			return "", "", "", false, rows.Err()
		}
		err = rows.Scan(&name, &checksum, &schema)
		return name, checksum, schema, true, err
	})
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	actual, err := schemaDigest(ctx, conn)
	if err != nil {
		return err
	}
	if actual != last {
		return fmt.Errorf("restored MySQL fixture schema fingerprint mismatch")
	}
	return nil
}

func validateFixtureLedger(names []string, next func() (name, checksum, schema string, ok bool, err error)) (string, error) {
	last := ""
	for _, want := range names {
		name, checksum, schema, ok, err := next()
		if err != nil {
			return "", err
		}
		if !ok || name != want || schema == "" {
			return "", fmt.Errorf("incomplete or unexpected fixture migration: expected %s, got %s", want, name)
		}
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			return "", err
		}
		matches, err := migrationChecksumMatches(data, checksum)
		if err != nil {
			return "", err
		}
		if !matches {
			return "", fmt.Errorf("fixture migration checksum mismatch: %s", name)
		}
		last = schema
	}
	if _, _, _, ok, err := next(); err != nil {
		return "", err
	} else if ok {
		return "", fmt.Errorf("unexpected extra fixture migration")
	}
	return last, nil
}

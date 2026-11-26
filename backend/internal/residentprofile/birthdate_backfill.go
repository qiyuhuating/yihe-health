package residentprofile

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

var ErrBirthdateBackfillIncomplete = errors.New("resident birthdate backfill is partial; retain the write barrier and rerun")

const birthdatePending = "birthdate_protected=false AND bothdate IS NOT NULL"
const birthdateCount = "SELECT COUNT(*) FROM oldperson WHERE " + birthdatePending
const birthdateBackfillColumns = "old_id,CAST(bothdate AS CHAR),birthdate_cipher,birthdate_protected"

// BackfillBirthdates authenticates the current inventory before writing.
// Apply requires stopped resident writers and a verified backup.
func BackfillBirthdates(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) (BackfillReport, error) {
	report := BackfillReport{Mode: "scan"}
	if apply {
		report.Mode = "apply"
	}
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return report, errors.New("valid database, key and bounded birthdate backfill required")
	}
	if err := database.QueryRowContext(ctx, birthdateCount).Scan(&report.Before); err != nil {
		return report, errors.New("cannot count pending resident birthdates")
	}
	if err := preflightBirthdates(ctx, database, box); err != nil {
		return report, err
	}
	if !apply {
		report.Remaining = &report.Before
		report.Complete = report.Before == 0
		return report, nil
	}
	var lastID int64
	for report.Migrated < int64(limit) {
		id, err := backfillBirthdate(ctx, database, box, lastID)
		if err != nil {
			return report, err
		}
		if id == 0 {
			break
		}
		lastID = id
		report.Migrated++
	}
	var remaining int64
	if err := database.QueryRowContext(ctx, birthdateCount).Scan(&remaining); err != nil {
		return report, errors.New("cannot verify remaining resident birthdates")
	}
	report.Remaining = &remaining
	report.Complete = remaining == 0
	if !report.Complete {
		return report, ErrBirthdateBackfillIncomplete
	}
	return report, nil
}

func scanBirthdate(scanner interface{ Scan(...any) error }) (int64, BirthdateRecord, error) {
	var id int64
	var raw *string
	var record BirthdateRecord
	if err := scanner.Scan(&id, &raw, &record.Cipher, &record.Protected); err != nil {
		return 0, record, err
	}
	if raw != nil {
		if id < 1 {
			return 0, record, errors.New("unsupported resident identity in birthdate backfill")
		}
		date, err := parseBirthdate(*raw)
		if err != nil {
			return 0, record, err
		}
		record.Legacy = &date
	}
	return id, record, nil
}

func preflightBirthdates(ctx context.Context, database *sql.DB, box *securestore.Box) error {
	rows, err := database.QueryContext(ctx, "SELECT "+birthdateBackfillColumns+" FROM oldperson ORDER BY old_id")
	if err != nil {
		return errors.New("cannot preflight resident birthdates")
	}
	defer rows.Close()
	for rows.Next() {
		id, record, err := scanBirthdate(rows)
		if err == nil {
			_, err = record.Open(box, id)
		}
		if err != nil {
			return errors.New("resident birthdate preflight failed before writes")
		}
	}
	if err := rows.Err(); err != nil {
		return errors.New("resident birthdate preflight did not complete")
	}
	return nil
}

func backfillBirthdate(ctx context.Context, database *sql.DB, box *securestore.Box, after int64) (int64, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, errors.New("cannot begin resident birthdate backfill")
	}
	defer tx.Rollback()
	id, record, err := scanBirthdate(tx.QueryRowContext(ctx,
		"SELECT "+birthdateBackfillColumns+" FROM oldperson WHERE old_id>? AND "+birthdatePending+" ORDER BY old_id LIMIT 1 FOR UPDATE", after))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil || record.Legacy == nil || record.Protected || record.Cipher != nil {
		return 0, errors.New("cannot lock and verify legacy resident birthdate")
	}
	if err := StoreBirthdate(ctx, tx, box, id, *record.Legacy); err != nil {
		return 0, errors.New("cannot protect legacy resident birthdate")
	}
	if err := tx.Commit(); err != nil {
		return 0, errors.New("cannot confirm resident birthdate backfill commit")
	}
	return id, nil
}

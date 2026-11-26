package guardiancontact

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

var ErrBackfillIncomplete = errors.New("guardian contact backfill is partial; retain the write barrier and rerun")

type BackfillReport struct {
	Mode      string `json:"mode"`
	Before    int64  `json:"pending_before"`
	Migrated  int64  `json:"migrated"`
	Remaining *int64 `json:"remaining"`
	Complete  bool   `json:"complete"`
}

const pending = `contact_protected=false AND (customer_phone IS NOT NULL OR customer_address IS NOT NULL)`

const recordColumns = `customer_id,customer_phone,customer_address,contact_cipher,contact_protected`

// Backfill requires stopped guardian writers and a verified backup for
// apply. NULL-only legacy contacts need no rewrite. No contact value is used as a
// relationship or uniqueness key, so duplicates and exact values are preserved.
func Backfill(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) (BackfillReport, error) {
	report := BackfillReport{Mode: "scan"}
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return report, errors.New("valid database, key and bounded guardian backfill required")
	}
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM customer WHERE "+pending).Scan(&report.Before); err != nil {
		return report, errors.New("cannot count pending guardian contacts")
	}
	if err := preflight(ctx, database, box); err != nil {
		return report, err
	}
	if !apply {
		report.Remaining = &report.Before
		report.Complete = report.Before == 0
		return report, nil
	}
	report.Mode = "apply"
	for report.Migrated < int64(limit) {
		changed, err := backfillOne(ctx, database, box)
		if err != nil {
			return report, err
		}
		if !changed {
			break
		}
		report.Migrated++
	}
	var remaining int64
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM customer WHERE "+pending).Scan(&remaining); err != nil {
		return report, errors.New("cannot verify remaining guardian contacts")
	}
	report.Remaining = &remaining
	report.Complete = remaining == 0
	if !report.Complete {
		return report, ErrBackfillIncomplete
	}
	return report, nil
}

func scanRecord(scanner interface{ Scan(...any) error }) (int64, Record, error) {
	var id int64
	var record Record
	err := scanner.Scan(&id, &record.Legacy.Phone, &record.Legacy.Address, &record.Cipher, &record.Protected)
	return id, record, err
}

func preflight(ctx context.Context, database *sql.DB, box *securestore.Box) error {
	if err := VerifyKey(ctx, database, box); err != nil {
		return err
	}
	rows, err := database.QueryContext(ctx, "SELECT "+recordColumns+" FROM customer ORDER BY customer_id")
	if err != nil {
		return errors.New("cannot preflight guardian contacts")
	}
	for rows.Next() {
		id, record, err := scanRecord(rows)
		if err == nil {
			_, err = record.Open(box, id)
		}
		if err != nil {
			rows.Close()
			return errors.New("guardian contact authentication or metadata failed; no rows migrated")
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("guardian contact preflight did not complete")
	}
	return nil
}

func backfillOne(ctx context.Context, database *sql.DB, box *securestore.Box) (bool, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, errors.New("cannot begin guardian contact backfill")
	}
	defer tx.Rollback()
	if err := PrepareWrite(ctx, tx, box); err != nil {
		return false, err
	}
	id, record, err := scanRecord(tx.QueryRowContext(ctx, "SELECT "+recordColumns+" FROM customer WHERE "+pending+" ORDER BY customer_id LIMIT 1 FOR UPDATE"))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("cannot lock pending guardian contact")
	}
	values, err := record.Open(box, id)
	if err != nil || record.Protected || !values.HasValues() {
		return false, errors.New("guardian contact changed during backfill")
	}
	if err := Store(ctx, tx, box, id, values); err != nil {
		return false, errors.New("cannot protect legacy guardian contact")
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New("cannot confirm guardian contact backfill commit")
	}
	return true, nil
}

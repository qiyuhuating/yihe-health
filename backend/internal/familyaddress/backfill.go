package familyaddress

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

var ErrBackfillIncomplete = errors.New("family address backfill is partial; retain the write barrier and rerun")

type BackfillReport struct {
	Mode      string `json:"mode"`
	Before    int64  `json:"pending_before"`
	Migrated  int64  `json:"migrated"`
	Remaining *int64 `json:"remaining"`
	Complete  bool   `json:"complete"`
}

const pending = "family_address_protected=false AND family_address IS NOT NULL"
const countPending = "SELECT COUNT(*) FROM family WHERE " + pending

// Backfill authenticates all existing records before applying bounded writes.
// Apply requires stopped family writers and a tested matching backup.
func Backfill(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) (BackfillReport, error) {
	report := BackfillReport{Mode: "scan"}
	if apply {
		report.Mode = "apply"
	}
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return report, errors.New("valid database, key and bounded family address backfill required")
	}
	if err := database.QueryRowContext(ctx, countPending).Scan(&report.Before); err != nil {
		return report, errors.New("cannot count pending family addresses")
	}
	if err := preflight(ctx, database, box); err != nil {
		return report, err
	}
	if !apply {
		report.Remaining = &report.Before
		report.Complete = report.Before == 0
		return report, nil
	}
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
	if err := database.QueryRowContext(ctx, countPending).Scan(&remaining); err != nil {
		return report, errors.New("cannot verify remaining family addresses")
	}
	report.Remaining = &remaining
	report.Complete = remaining == 0
	if !report.Complete {
		return report, ErrBackfillIncomplete
	}
	return report, nil
}

func preflight(ctx context.Context, database *sql.DB, box *securestore.Box) error {
	rows, err := database.QueryContext(ctx, "SELECT family_id,"+Columns+" FROM family ORDER BY family_id")
	if err != nil {
		return errors.New("cannot scan family addresses")
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var record Record
		if err := rows.Scan(&id, &record.Legacy, &record.Cipher, &record.Protected); err != nil {
			return errors.New("cannot read family address metadata")
		}
		if _, err := record.Open(box, id); err != nil {
			return errors.New("family address preflight failed before writes")
		}
	}
	if err := rows.Err(); err != nil {
		return errors.New("family address preflight did not complete")
	}
	return nil
}

func backfillOne(ctx context.Context, database *sql.DB, box *securestore.Box) (bool, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, errors.New("cannot begin family address backfill")
	}
	defer tx.Rollback()
	var id int64
	var record Record
	err = tx.QueryRowContext(ctx, "SELECT family_id,"+Columns+" FROM family WHERE "+pending+" ORDER BY family_id LIMIT 1 FOR UPDATE").
		Scan(&id, &record.Legacy, &record.Cipher, &record.Protected)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("cannot lock pending family address")
	}
	address, err := record.Open(box, id)
	if err != nil || address == nil || record.Protected {
		return false, errors.New("family address changed during backfill")
	}
	if err := Store(ctx, tx, box, id, *address); err != nil {
		return false, errors.New("cannot protect legacy family address")
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New("cannot confirm family address backfill commit")
	}
	return true, nil
}

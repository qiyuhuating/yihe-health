package residentprofile

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

var ErrBackfillIncomplete = errors.New("resident care profile backfill is partial; retain the write barrier and rerun")

type BackfillReport struct {
	Mode      string `json:"mode"`
	Before    int64  `json:"pending_before"`
	Migrated  int64  `json:"migrated"`
	Remaining *int64 `json:"remaining"`
	Complete  bool   `json:"complete"`
}

const pending = `care_profile_protected=false AND
 (phone IS NOT NULL OR room IS NOT NULL OR illness IS NOT NULL OR warn IS NOT NULL OR
 food_like IS NOT NULL OR food_hate IS NOT NULL OR drag IS NOT NULL)`

const recordColumns = `old_id,phone,room,illness,warn,food_like,food_hate,drag,care_profile_cipher,phone_lookup,care_profile_protected`

// Backfill requires stopped resident/family writers and a verified backup for
// apply. NULL-only legacy profiles need no rewrite. Duplicate contact phones are
// preserved: they remain ambiguous for the existing family association API.
func Backfill(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) (BackfillReport, error) {
	report := BackfillReport{Mode: "scan"}
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return report, errors.New("valid database, key and bounded resident backfill required")
	}
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM oldperson WHERE "+pending).Scan(&report.Before); err != nil {
		return report, errors.New("cannot count pending resident care profiles")
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
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM oldperson WHERE "+pending).Scan(&remaining); err != nil {
		return report, errors.New("cannot verify remaining resident care profiles")
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
	err := scanner.Scan(&id, &record.Legacy.Phone, &record.Legacy.Room, &record.Legacy.Illness, &record.Legacy.Warn,
		&record.Legacy.FoodLike, &record.Legacy.FoodHate, &record.Legacy.Drag, &record.Cipher, &record.Lookup, &record.Protected)
	return id, record, err
}

func preflight(ctx context.Context, database *sql.DB, box *securestore.Box) error {
	if err := VerifyKey(ctx, database, box); err != nil {
		return err
	}
	rows, err := database.QueryContext(ctx, "SELECT "+recordColumns+" FROM oldperson ORDER BY old_id")
	if err != nil {
		return errors.New("cannot preflight resident care profiles")
	}
	for rows.Next() {
		id, record, err := scanRecord(rows)
		if err == nil {
			_, err = record.Open(box, id)
		}
		if err != nil {
			rows.Close()
			return errors.New("resident profile authentication or metadata failed; no rows migrated")
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("resident profile preflight did not complete")
	}
	return nil
}

func backfillOne(ctx context.Context, database *sql.DB, box *securestore.Box) (bool, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, errors.New("cannot begin resident profile backfill")
	}
	defer tx.Rollback()
	if err := PrepareWrite(ctx, tx, box); err != nil {
		return false, err
	}
	id, record, err := scanRecord(tx.QueryRowContext(ctx, "SELECT "+recordColumns+" FROM oldperson WHERE "+pending+" ORDER BY old_id LIMIT 1 FOR UPDATE"))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("cannot lock pending resident profile")
	}
	values, err := record.Open(box, id)
	if err != nil || record.Protected || !values.HasValues() {
		return false, errors.New("resident profile changed during backfill")
	}
	if err := Store(ctx, tx, box, id, values); err != nil {
		return false, errors.New("cannot protect legacy resident profile")
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New("cannot confirm resident profile backfill commit")
	}
	return true, nil
}

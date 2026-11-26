package userphone

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

var ErrFamilyBackfillIncomplete = errors.New("family phone backfill is partial; retain the write barrier and rerun")

type FamilyBackfillReport struct {
	Mode      string `json:"mode"`
	Before    int64  `json:"pending_before"`
	Migrated  int64  `json:"migrated"`
	Remaining *int64 `json:"remaining"`
	Complete  bool   `json:"complete"`
}

// BackfillFamily uses one transaction per legacy row. Stop account writers and
// verify a backup first; neither this bounded scan nor key rotation is a barrier.
func BackfillFamily(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) (FamilyBackfillReport, error) {
	report := FamilyBackfillReport{Mode: "scan"}
	if database == nil || limit < 1 || limit > 1000 || (apply && box == nil) {
		return report, errors.New("valid database, key and bounded family backfill required")
	}
	const pending = "SELECT COUNT(*) FROM family WHERE family_phone_protected=false AND family_phone IS NOT NULL"
	if err := database.QueryRowContext(ctx, pending).Scan(&report.Before); err != nil {
		return report, errors.New("cannot count legacy family phones")
	}
	if !apply {
		report.Remaining = &report.Before
		report.Complete = report.Before == 0
		return report, nil
	}
	report.Mode = "apply"
	if err := VerifyLookupKey(ctx, database, box); err != nil {
		return report, errors.New("family lookup key cannot be verified")
	}
	rows, err := database.QueryContext(ctx, `SELECT family_id,family_phone,family_phone_cipher,family_phone_lookup,family_phone_protected
 FROM family WHERE family_phone_protected=true ORDER BY family_id`)
	if err != nil {
		return report, errors.New("cannot preflight protected family phones")
	}
	for rows.Next() {
		var id int64
		var record Record
		if err := rows.Scan(&id, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected); err != nil {
			rows.Close()
			return report, errors.New("cannot read protected family phone")
		}
		if _, err := record.OpenFamilyStored(box, id); err != nil {
			rows.Close()
			return report, errors.New("protected family phone authentication failed; no legacy rows changed")
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return report, errors.New("family phone preflight did not complete")
	}
	for report.Migrated < int64(limit) {
		changed, err := backfillFamilyOne(ctx, database, box)
		if err != nil {
			return report, err
		}
		if !changed {
			break
		}
		report.Migrated++
	}
	var remaining int64
	if err := database.QueryRowContext(ctx, pending).Scan(&remaining); err != nil {
		return report, errors.New("cannot verify remaining family phones")
	}
	report.Remaining = &remaining
	report.Complete = remaining == 0
	if !report.Complete {
		return report, ErrFamilyBackfillIncomplete
	}
	return report, nil
}

func backfillFamilyOne(ctx context.Context, database *sql.DB, box *securestore.Box) (bool, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return false, errors.New("cannot begin family phone backfill")
	}
	defer tx.Rollback()
	var id int64
	var record Record
	err = tx.QueryRowContext(ctx, `SELECT family_id,family_phone,family_phone_cipher,family_phone_lookup,family_phone_protected FROM family
 WHERE family_phone_protected=false AND family_phone IS NOT NULL ORDER BY family_id LIMIT 1 FOR UPDATE`).Scan(&id, &record.Legacy, &record.Cipher, &record.Lookup, &record.Protected)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("cannot lock legacy family phone")
	}
	opened, err := record.OpenFamilyStored(box, id)
	if err != nil || opened == nil {
		return false, errors.New("legacy family phone metadata requires correction before backfill")
	}
	phone := *opened
	lookup, err := SearchIndex(ctx, tx, box, phone)
	if err != nil {
		return false, errors.New("family phone or lookup key requires correction before backfill")
	}
	var conflict bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE
 (user_phone_protected=false AND user_phone=?) OR (user_phone_protected=true AND user_phone_lookup=?))
 OR EXISTS(SELECT 1 FROM family WHERE `+FamilyMatch+` AND family_id<>?)`, phone, lookup, phone, lookup, id).Scan(&conflict)
	if err != nil || conflict {
		return false, errors.New("family phone conflicts with another account or uniqueness cannot be verified")
	}
	if err := StoreFamily(ctx, tx, box, id, phone); err != nil {
		return false, errors.New("cannot protect legacy family phone")
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New("cannot confirm family phone backfill commit")
	}
	return true, nil
}

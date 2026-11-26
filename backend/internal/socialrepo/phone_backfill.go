package socialrepo

import (
	"context"
	"database/sql"
	"errors"
	"regexp"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
)

var ErrPhoneBackfillIncomplete = errors.New("social phone backfill is partial; retain the write barrier and rerun")

type PhoneBackfillReport struct {
	Mode      string `json:"mode"`
	Before    int64  `json:"pending_before"`
	Migrated  int64  `json:"migrated"`
	Remaining *int64 `json:"remaining"`
	Complete  bool   `json:"complete"`
}

const pendingSocialPhones = `SELECT
 (SELECT COUNT(*) FROM said WHERE said_phone_protected=false AND said_phone IS NOT NULL)+
 (SELECT COUNT(*) FROM nice_count WHERE phone_protected=false AND phone IS NOT NULL)`

var phoneLookupPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BackfillPhones requires stopped social writers and a verified backup before
// apply. The full read-only preflight must succeed before the first row changes.
// Each durable row transaction rechecks under locks. NULL legacy identities are
// retained as unattributed rows, not fabricated or silently removed.
func BackfillPhones(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) (PhoneBackfillReport, error) {
	report := PhoneBackfillReport{Mode: "scan"}
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return report, errors.New("valid database, key and bounded social phone backfill required")
	}
	if err := database.QueryRowContext(ctx, pendingSocialPhones).Scan(&report.Before); err != nil {
		return report, errors.New("cannot count legacy social phones")
	}
	if err := preflightSocialPhones(ctx, database, box); err != nil {
		return report, err
	}
	if !apply {
		report.Remaining = &report.Before
		report.Complete = report.Before == 0
		return report, nil
	}
	report.Mode = "apply"
	// Bound retries as well as successful writes under accidental concurrent churn.
	for attempts := 0; report.Migrated < int64(limit) && attempts < limit*4; attempts++ {
		changed, available, err := backfillSocialPhoneOne(ctx, database, box)
		if err != nil {
			return report, err
		}
		if changed {
			report.Migrated++
		}
		if !available {
			break
		}
	}
	var remaining int64
	if err := database.QueryRowContext(ctx, pendingSocialPhones).Scan(&remaining); err != nil {
		return report, errors.New("cannot verify remaining social phones")
	}
	report.Remaining = &remaining
	report.Complete = remaining == 0
	if !report.Complete {
		return report, ErrPhoneBackfillIncomplete
	}
	return report, nil
}

func preflightSocialPhones(ctx context.Context, database *sql.DB, box *securestore.Box) error {
	if err := verifySocialPhoneKey(ctx, database, box); err != nil {
		return err
	}
	rows, err := database.QueryContext(ctx, `SELECT said_id,said_phone,said_phone_cipher,said_phone_protected FROM said ORDER BY said_id`)
	if err != nil {
		return errors.New("cannot preflight comment phones")
	}
	for rows.Next() {
		var id int64
		var legacy, cipher *string
		var protected bool
		if err := rows.Scan(&id, &legacy, &cipher, &protected); err != nil {
			rows.Close()
			return errors.New("cannot read comment phone metadata")
		}
		phone, err := openCommentPhone(box, id, legacy, cipher, protected)
		if err != nil || (phone != nil && !auth.ValidPhone(*phone)) {
			rows.Close()
			return errors.New("comment phone requires correction before backfill")
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("comment phone preflight did not complete")
	}
	rows, err = database.QueryContext(ctx, `SELECT n.add_id,n.said_id,n.phone,n.phone_lookup,n.phone_protected,s.said_id
 FROM nice_count n LEFT JOIN said s ON s.said_id=n.said_id ORDER BY n.add_id`)
	if err != nil {
		return errors.New("cannot preflight like identities")
	}
	for rows.Next() {
		var id int64
		var parent, existingParent *int64
		var phone, lookup *string
		var protected bool
		if err := rows.Scan(&id, &parent, &phone, &lookup, &protected, &existingParent); err != nil {
			rows.Close()
			return errors.New("cannot read like identity metadata")
		}
		valid := !protected && lookup == nil && (phone == nil || auth.ValidPhone(*phone))
		if protected {
			valid = phone == nil && lookup != nil && phoneLookupPattern.MatchString(*lookup)
		}
		if !valid || ((phone != nil || protected) && (parent == nil || existingParent == nil)) {
			rows.Close()
			return errors.New("like identity or orphan requires correction before backfill")
		}
		if phone != nil {
			var count int64
			if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM nice_count WHERE said_id=? AND "+likePhoneMatch,
				*parent, *phone, box.Index("social-like-phone", *phone)).Scan(&count); err != nil || count != 1 {
				rows.Close()
				return errors.New("duplicate legacy or mixed votes require review before backfill")
			}
		}
	}
	readErr = rows.Err()
	closeErr = rows.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("like identity preflight did not complete")
	}
	var inconsistent bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM said s
 WHERE EXISTS(SELECT 1 FROM nice_count n WHERE n.said_id=s.said_id AND n.phone IS NOT NULL AND n.phone_protected=false)
 AND (s.nice_count IS NULL OR s.nice_count<>(SELECT COUNT(*) FROM nice_count n WHERE n.said_id=s.said_id)))`).Scan(&inconsistent); err != nil || inconsistent {
		return errors.New("vote aggregates require review before backfill")
	}
	return nil
}

func backfillSocialPhoneOne(ctx context.Context, database *sql.DB, box *securestore.Box) (changed, available bool, operationErr error) {
	var id, parent int64
	var phone string
	comment := true
	err := database.QueryRowContext(ctx, `SELECT said_id,said_phone FROM said
 WHERE said_phone_protected=false AND said_phone IS NOT NULL ORDER BY said_id LIMIT 1`).Scan(&id, &phone)
	if errors.Is(err, sql.ErrNoRows) {
		comment = false
		err = database.QueryRowContext(ctx, `SELECT add_id,said_id,phone FROM nice_count
 WHERE phone_protected=false AND phone IS NOT NULL ORDER BY add_id LIMIT 1`).Scan(&id, &parent, &phone)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, true, errors.New("cannot select legacy social phone")
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, true, errors.New("cannot begin social phone backfill")
	}
	defer tx.Rollback()
	if _, err = bindSocialPhoneKey(ctx, tx, box, phone); err != nil {
		return false, true, err
	}
	if comment {
		var legacy, cipher *string
		var protected bool
		err = tx.QueryRowContext(ctx, `SELECT said_phone,said_phone_cipher,said_phone_protected FROM said WHERE said_id=? FOR UPDATE`, id).Scan(&legacy, &cipher, &protected)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (protected || legacy == nil)) {
			return false, true, nil // Concurrently removed/migrated; do not overwrite.
		}
		if err != nil {
			return false, true, errors.New("cannot lock legacy comment phone")
		}
		opened, err := openCommentPhone(box, id, legacy, cipher, protected)
		if err != nil || opened == nil {
			return false, true, errors.New("legacy comment metadata changed during backfill")
		}
		sealed, err := sealCommentPhone(box, id, *opened)
		if err != nil {
			return false, true, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE said SET said_phone=NULL,said_phone_cipher=?,said_phone_protected=true
 WHERE said_id=? AND said_phone_protected=false AND said_phone=? AND said_phone_cipher IS NULL`, sealed, id, *opened)
		if err := requireOneSocialPhoneUpdate(result, err); err != nil {
			return false, true, err
		}
	} else {
		// Same lock order as ToggleLike: binding, parent comment, then vote row.
		var locked int64
		err = tx.QueryRowContext(ctx, "SELECT said_id FROM said WHERE said_id=? FOR UPDATE", parent).Scan(&locked)
		if errors.Is(err, sql.ErrNoRows) {
			return false, true, errors.New("orphan vote encountered during backfill")
		}
		if err != nil {
			return false, true, errors.New("cannot lock vote parent")
		}
		var currentParent *int64
		var legacy, index *string
		var protected bool
		err = tx.QueryRowContext(ctx, `SELECT said_id,phone,phone_lookup,phone_protected FROM nice_count WHERE add_id=? FOR UPDATE`, id).Scan(&currentParent, &legacy, &index, &protected)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (protected || legacy == nil)) {
			return false, true, nil
		}
		if err != nil || currentParent == nil || *currentParent != parent || index != nil || !auth.ValidPhone(*legacy) {
			return false, true, errors.New("legacy vote metadata changed during backfill")
		}
		lookup := box.Index("social-like-phone", *legacy)
		var count int64
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM nice_count WHERE said_id=? AND "+likePhoneMatch, parent, *legacy, lookup).Scan(&count); err != nil || count != 1 {
			return false, true, errors.New("duplicate votes encountered during backfill")
		}
		result, err := tx.ExecContext(ctx, `UPDATE nice_count SET phone=NULL,phone_lookup=?,phone_protected=true
 WHERE add_id=? AND said_id=? AND phone_protected=false AND phone=? AND phone_lookup IS NULL`, lookup, id, parent, *legacy)
		if err := requireOneSocialPhoneUpdate(result, err); err != nil {
			return false, true, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, true, errors.New("cannot confirm social phone backfill commit")
	}
	return true, true, nil
}

func requireOneSocialPhoneUpdate(result sql.Result, err error) error {
	if err != nil {
		return errors.New("cannot protect legacy social phone")
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return errors.New("legacy social phone update did not match exactly one row")
	}
	return nil
}

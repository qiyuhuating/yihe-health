package health

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// MaxRevision is the largest revision represented exactly by JSON clients.
const MaxRevision int64 = 1<<53 - 1

const maxSafeRevision = MaxRevision
const ownerRevisionRange int64 = 65536

type ownerClock struct {
	revision   int64
	rangeEnd   int64
	rangeStart int64
}

// LockOwner serializes sample writes and daily cache snapshots for one owner.
// Its lock lasts until the caller commits or rolls back the transaction.
func LockOwner(ctx context.Context, tx *sql.Tx, oldID int64) (int64, error) {
	clock, err := lockOwner(ctx, tx, oldID)
	return clock.revision, err
}

func lockOwner(ctx context.Context, tx *sql.Tx, oldID int64) (ownerClock, error) {
	var clock ownerClock
	err := tx.QueryRowContext(ctx, "SELECT revision,range_end,range_start FROM health_owner_clock WHERE old_id=? FOR UPDATE", oldID).Scan(&clock.revision, &clock.rangeEnd, &clock.rangeStart)
	if errors.Is(err, sql.ErrNoRows) {
		// A new owner has no reserved range. Read the existing high-water mark
		// without locking it; a later reservation always locks and rereads it.
		var high int64
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil {
			return clock, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO health_owner_clock(old_id,revision,range_end) VALUES(?,?,?) ON DUPLICATE KEY UPDATE old_id=old_id", oldID, high, high); err != nil {
			return clock, err
		}
		err = tx.QueryRowContext(ctx, "SELECT revision,range_end,range_start FROM health_owner_clock WHERE old_id=? FOR UPDATE", oldID).Scan(&clock.revision, &clock.rangeEnd, &clock.rangeStart)
	}
	if err == nil && (clock.revision < 0 || clock.rangeEnd < clock.revision || clock.rangeEnd > maxSafeRevision || clock.rangeStart < 0 || clock.rangeStart > clock.rangeEnd) {
		err = fmt.Errorf("health revision exceeds safe cursor range")
	}
	return clock, err
}

// reserve returns the revision immediately before this batch. A range is unique
// globally, while its owner lock preserves commit ordering within read cursors.
func (c *ownerClock) reserve(ctx context.Context, tx *sql.Tx, oldID int64, count int) (int64, error) {
	start, end := max(c.revision, c.rangeStart), c.rangeEnd
	if start < 0 || start > maxSafeRevision-int64(count) {
		return 0, fmt.Errorf("health revision exceeds safe cursor range")
	}
	if int64(count) > end-start {
		// UPDATE takes the global row lock and returns its previous value in
		// the same statement's OK packet. A guarded update cannot allocate
		// an overlapping or unsafe range. Rollback still undoes the water
		// mark; never read the connection's persistent LAST_INSERT_ID state.
		result, err := tx.ExecContext(ctx, "UPDATE health_clock SET revision=LEAST(?,LAST_INSERT_ID(revision)+?) WHERE id=1 AND revision>=? AND revision<=?",
			maxSafeRevision, ownerRevisionRange, c.rangeEnd, maxSafeRevision-int64(count))
		if err != nil {
			return 0, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if affected != 1 {
			return 0, fmt.Errorf("health revision exceeds safe cursor range")
		}
		start, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
		end = min(maxSafeRevision, start+ownerRevisionRange)
	}
	_, err := tx.ExecContext(ctx, "UPDATE health_owner_clock SET revision=?,range_end=? WHERE old_id=?", start+int64(count), end, oldID)
	c.rangeEnd = end
	return start, err
}

// reserveNext moves the normal range-refill lock to the end of the previous
// successful batch. The owner cursor still names its last actual event; the
// separately stored range floor prevents issuance of intervening revisions.
// Near exhaustion, retain the old allocator's partial-range behavior.
func (c ownerClock) reserveNext(ctx context.Context, tx *sql.Tx, oldID, revision int64) error {
	if c.rangeEnd-revision >= 200 || c.rangeEnd > maxSafeRevision-ownerRevisionRange {
		return nil
	}
	result, err := tx.ExecContext(ctx, "UPDATE health_clock SET revision=LAST_INSERT_ID(revision)+? WHERE id=1 AND revision>=? AND revision<=?", ownerRevisionRange, c.rangeEnd, maxSafeRevision-ownerRevisionRange)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return err
	}
	start, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE health_owner_clock SET range_start=?,range_end=? WHERE old_id=?", start, start+ownerRevisionRange, oldID)
	return err
}

// ProvisionOwnerRange is called only for a newly inserted resident, at the
// end of its creation transaction. A future range is durable together with
// the resident, but the committed event cursor remains zero until a write.
func ProvisionOwnerRange(ctx context.Context, tx *sql.Tx, oldID int64) error {
	if _, err := tx.ExecContext(ctx, "INSERT INTO health_owner_clock(old_id,revision,range_end) VALUES(?,0,0)", oldID); err != nil {
		return err
	}
	return (ownerClock{}).reserveNext(ctx, tx, oldID, 0)
}

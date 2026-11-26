package vitalrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/securestore"
)

var ErrMeasurementBackfillIncomplete = errors.New("vital measurement backfill is partial; retain the write barrier and rerun")

type MeasurementBackfillReport struct {
	Before    int64
	Migrated  int
	Remaining *int64
}

const measurementRowColumns = "id,old_id,device_id,hr,bp,measurement_cipher"
const pendingMeasurementsCount = "SELECT COUNT(*) FROM ratepre WHERE measurement_pending=true"

type measurementRow struct {
	id            int64
	owner, device *int64
	stored        StoredMeasurements
}

func readMeasurementRow(row *sql.Row) (measurementRow, error) {
	var record measurementRow
	err := row.Scan(&record.id, &record.owner, &record.device, &record.stored.HeartRate, &record.stored.Pressure, &record.stored.Cipher)
	return record, err
}

// BackfillMeasurements migrates legacy rows in bounded transactions following
// a write barrier, a verified backup and a full encryption-reader inventory.
func BackfillMeasurements(ctx context.Context, database *sql.DB, box *securestore.Box, limit int, apply bool) (MeasurementBackfillReport, error) {
	var report MeasurementBackfillReport
	if database == nil || box == nil || limit < 1 || limit > 1000 {
		return report, errors.New("valid database, key and bounded vital measurement backfill required")
	}
	if err := database.QueryRowContext(ctx, pendingMeasurementsCount).Scan(&report.Before); err != nil {
		return report, errors.New("cannot count pending vital measurements")
	}
	check, err := readMeasurementRow(database.QueryRowContext(ctx,
		"SELECT "+measurementRowColumns+" FROM ratepre WHERE measurement_cipher IS NOT NULL ORDER BY id LIMIT 1"))
	if err == nil {
		_, err = check.stored.Open(box, check.id, check.owner, check.device)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return report, errors.New("cannot verify the current vital measurement reader")
	}
	if !apply {
		report.Remaining = &report.Before
		return report, nil
	}
	for report.Migrated < limit {
		processed, err := protectNextMeasurement(ctx, database, box)
		if err != nil {
			return report, err
		}
		if !processed {
			break
		}
		report.Migrated++
	}
	var remaining int64
	if err := database.QueryRowContext(ctx, pendingMeasurementsCount).Scan(&remaining); err != nil {
		return report, errors.New("cannot verify remaining vital measurements")
	}
	report.Remaining = &remaining
	if remaining != 0 {
		return report, ErrMeasurementBackfillIncomplete
	}
	return report, nil
}

func protectNextMeasurement(ctx context.Context, database *sql.DB, box *securestore.Box) (bool, error) {
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, errors.New("cannot begin vital measurement backfill")
	}
	defer tx.Rollback()
	record, err := readMeasurementRow(tx.QueryRowContext(ctx,
		"SELECT "+measurementRowColumns+" FROM ratepre WHERE measurement_pending=true ORDER BY id LIMIT 1 FOR UPDATE"))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || record.stored.Cipher != nil {
		return false, errors.New("cannot lock a plaintext vital measurement")
	}
	if err := StoreProtectedMeasurements(ctx, tx, box, record.id, record.owner, record.device, record.stored.Measurements); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, errors.New("cannot confirm vital measurement backfill commit")
	}
	return true, nil
}

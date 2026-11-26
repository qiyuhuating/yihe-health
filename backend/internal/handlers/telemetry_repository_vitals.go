package handlers

import (
	"context"
	"database/sql"

	"yihe-care/internal/devicerepo"
	"yihe-care/internal/telemetryrepo"
	"yihe-care/internal/vitalpolicy"
)

func measurementState(value *int, low, high int) any {
	if value == nil {
		return nil
	}
	if *value < low || *value > high {
		return "1"
	}
	return "0"
}

func (repo telemetryRepository) persistReportVitals(ctx context.Context, tx *sql.Tx, d *devicerepo.DeviceRecord, oldID int64, parsed *parsedDeviceTelemetry) (telemetryPersisted, error) {
	hr, bp := parsed.hr, parsed.bp
	// Zero vital signs are legacy "not measured" sentinels.
	if hr != nil && *hr == 0 {
		hr = nil
	}
	if bp != nil && *bp == 0 {
		bp = nil
	}
	hasMeasurements := (hr != nil && *hr > 0) || (bp != nil && *bp > 0)
	var bounds telemetryrepo.Thresholds
	if hasMeasurements {
		var err error
		bounds, err = telemetryrepo.ReadThresholds(ctx, tx, d.ID)
		if err != nil {
			return telemetryPersisted{}, err
		}
	}
	hrLow, hrHigh := vitalpolicy.Thresholds(bounds.HeartRateLow, bounds.HeartRateHigh, 55, 110)
	bpLow, bpHigh := vitalpolicy.Thresholds(bounds.PressureLow, bounds.PressureHigh, 90, 140)
	result := telemetryPersisted{rateState: measurementState(hr, hrLow, hrHigh), preState: measurementState(bp, bpLow, bpHigh)}
	if !hasMeasurements {
		return result, nil
	}
	if err := telemetryrepo.InsertVitals(ctx, tx, d.ID, oldID, d.HealthEpoch, parsed.beTime, hr, bp, result.rateState, result.preState, repo.box); err != nil {
		return telemetryPersisted{}, err
	}
	return result, nil
}

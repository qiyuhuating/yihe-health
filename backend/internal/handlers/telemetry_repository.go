package handlers

import (
	"context"
	"database/sql"
	"strings"

	"yihe-care/internal/devicerepo"
	"yihe-care/internal/securestore"
	"yihe-care/internal/telemetryrepo"
)

// telemetryRepository owns SQL for the existing device report contract.
// Call sites select native SQL for engine-specific upserts; placeholders remain portable.
type telemetryRepository struct {
	box *securestore.Box
}

type telemetryPersisted struct {
	rateState      any
	preState       any
	careIncidentID int64
}

func (repo telemetryRepository) PersistDeviceReport(ctx context.Context, tx *sql.Tx, d *devicerepo.DeviceRecord, parsed parsedDeviceTelemetry) (telemetryPersisted, error) {
	oldID := *d.OldID
	var position string
	if parsed.lng != "" || parsed.alarm == "1" || strings.EqualFold(parsed.alarm, "fall") {
		var err error
		position, err = sealPositionMetadata(repo.box, parsed.position)
		if err != nil {
			return telemetryPersisted{}, err
		}
	}
	result, err := repo.persistReportVitals(ctx, tx, d, oldID, &parsed)
	if err != nil {
		return telemetryPersisted{}, err
	}
	if err := telemetryrepo.PersistDay(ctx, tx, d.ID, oldID, d.HealthEpoch, parsed.day, parsed.beTime, parsed.step, parsed.sleep, parsed.sleepState); err != nil {
		return telemetryPersisted{}, err
	}
	incidentID, err := repo.persistReportPositionAndAlarm(ctx, tx, d, oldID, &parsed, position)
	if err != nil {
		return telemetryPersisted{}, err
	}
	result.careIncidentID = incidentID
	if err := telemetryrepo.TouchDevice(ctx, tx, d.ID, d.HealthEpoch, parsed.battery, parsed.appVersion, parsed.now); err != nil {
		return telemetryPersisted{}, err
	}
	return result, nil
}

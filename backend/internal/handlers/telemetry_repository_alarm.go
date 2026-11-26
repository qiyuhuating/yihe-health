package handlers

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"yihe-care/internal/devicerepo"
	"yihe-care/internal/notify"
	"yihe-care/internal/telemetryrepo"
)

func (repo telemetryRepository) persistReportPositionAndAlarm(ctx context.Context, tx *sql.Tx, d *devicerepo.DeviceRecord, oldID int64, parsed *parsedDeviceTelemetry, position string) (int64, error) {
	lng, lat, sourceStr, alarm, beTime := parsed.lng, parsed.lat, parsed.sourceStr, parsed.alarm, parsed.beTime
	if lng != "" && lat != "" {
		location := telemetryrepo.Location{
			DeviceID: d.ID, ResidentID: oldID,
			Longitude: sealCoordinate(repo.box, lng), Latitude: sealCoordinate(repo.box, lat),
			Source: sourceStr, ObservedAt: beTime, PositionMetadata: position,
		}
		if err := telemetryrepo.InsertLocation(ctx, tx, location); err != nil {
			return 0, err
		}
		if err := telemetryrepo.UpdateLatestLocation(ctx, tx, location); err != nil {
			return 0, err
		}
	}
	if alarm != "1" && !strings.EqualFold(alarm, "fall") {
		return 0, nil
	}
	kind := "unknown"
	if strings.EqualFold(alarm, "fall") {
		kind = "fall"
	}
	alarmID, err := telemetryrepo.InsertAlarm(ctx, tx, telemetryrepo.Alarm{
		ResidentID: oldID, DeviceID: d.ID, ObservedAt: beTime,
		Longitude: coordinateArg(repo.box, lng), Latitude: coordinateArg(repo.box, lat),
		Kind: kind, PositionMetadata: position,
	})
	if err != nil {
		return 0, err
	}
	incidentKind := "device_alarm"
	if kind == "fall" {
		incidentKind = "fall"
	}
	var requestValue any
	if parsed.requestID != "" {
		requestValue = parsed.requestID
	}
	incidentID, err := telemetryrepo.InsertIncident(ctx, tx, telemetryrepo.Incident{
		AlarmID: alarmID, ResidentID: oldID, DeviceID: d.ID,
		Kind: incidentKind, RequestID: requestValue,
	})
	if err != nil {
		return 0, err
	}
	if err := telemetryrepo.InsertAlarmEvent(ctx, tx, incidentID); err != nil {
		return 0, err
	}
	if _, err := notify.EnqueueIncident(ctx, tx, repo.box, incidentID, oldID); err != nil {
		return 0, err
	}
	return incidentID, nil
}

// SOS incident, event and notification records must commit before the device is acknowledged.
func (repo telemetryRepository) PersistSOSIncident(ctx context.Context, tx *sql.Tx, d *devicerepo.DeviceRecord, lng, lat, requestID string, meta positionMetadata, now time.Time) (int64, error) {
	position, err := sealPositionMetadata(repo.box, meta)
	if err != nil {
		return 0, err
	}
	alarmID, err := telemetryrepo.InsertAlarm(ctx, tx, telemetryrepo.Alarm{
		ResidentID: *d.OldID, DeviceID: d.ID, ObservedAt: now,
		Longitude: coordinateArg(repo.box, lng), Latitude: coordinateArg(repo.box, lat),
		Kind: "sos", PositionMetadata: position,
	})
	if err != nil {
		return 0, err
	}
	if lng != "" && lat != "" {
		if err := telemetryrepo.InsertLocation(ctx, tx, telemetryrepo.Location{
			DeviceID: d.ID, ResidentID: *d.OldID,
			Longitude: sealCoordinate(repo.box, lng), Latitude: sealCoordinate(repo.box, lat),
			Source: "sos", ObservedAt: now, PositionMetadata: position,
		}); err != nil {
			return 0, err
		}
	}
	if err := telemetryrepo.TouchDevice(ctx, tx, d.ID, d.HealthEpoch, nil, nil, now); err != nil {
		return 0, err
	}
	var requestValue any
	if requestID != "" {
		requestValue = requestID
	}
	incidentID, err := telemetryrepo.InsertSOSIncident(ctx, tx, alarmID, *d.OldID, d.ID, requestValue)
	if err != nil {
		return 0, err
	}
	if err := telemetryrepo.InsertSOSEvent(ctx, tx, incidentID); err != nil {
		return 0, err
	}
	return incidentID, nil
}

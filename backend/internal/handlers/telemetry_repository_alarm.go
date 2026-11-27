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

type preparedSOSPosition struct {
	alarm    telemetryrepo.Alarm
	location *telemetryrepo.Location
}

func (repo telemetryRepository) prepareSOSPosition(lng, lat string, meta positionMetadata) (preparedSOSPosition, error) {
	position, err := sealPositionMetadata(repo.box, meta)
	if err != nil {
		return preparedSOSPosition{}, err
	}
	prepared := preparedSOSPosition{alarm: telemetryrepo.Alarm{
		Longitude: coordinateArg(repo.box, lng), Latitude: coordinateArg(repo.box, lat),
		Kind: "sos", PositionMetadata: position,
	}}
	if lng != "" && lat != "" {
		prepared.location = &telemetryrepo.Location{
			Longitude: sealCoordinate(repo.box, lng), Latitude: sealCoordinate(repo.box, lat),
			Source: "sos", PositionMetadata: position,
		}
	}
	return prepared, nil
}

// SOS incident, event and notification records must commit before the device is acknowledged.
func (repo telemetryRepository) PersistSOSIncident(ctx context.Context, tx *sql.Tx, d *devicerepo.DeviceRecord, requestID string, prepared preparedSOSPosition, now time.Time) (int64, error) {
	alarm := prepared.alarm
	alarm.ResidentID, alarm.DeviceID, alarm.ObservedAt = *d.OldID, d.ID, now
	alarmID, err := telemetryrepo.InsertAlarm(ctx, tx, alarm)
	if err != nil {
		return 0, err
	}
	if prepared.location != nil {
		location := *prepared.location
		location.DeviceID, location.ResidentID, location.ObservedAt = d.ID, *d.OldID, now
		if err := telemetryrepo.InsertLocation(ctx, tx, location); err != nil {
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

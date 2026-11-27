package lane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"yihe-care/internal/deviceprotocol"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/securestore"
	"yihe-care/internal/telemetryrepo"
	"yihe-care/internal/telemetryworkflow"
)

const brokerEventScope = "yihe-next-telemetry-event-v1"

// TelemetryEvent is issued only by a trusted, authenticated producer. The
// entire identity and payload are sealed together, not merely the metrics.
// A retry must retain the originally issued event, including its timestamps.
type TelemetryEvent struct {
	DeviceID       int64
	ResidentID     int64
	BindingEpoch   int64
	AuthGeneration int64
	RequestID      string
	Payload        telemetryworkflow.Payload
}

func (e TelemetryEvent) validate() error {
	p := e.Payload
	if e.DeviceID <= 0 || e.ResidentID <= 0 || e.BindingEpoch <= 0 ||
		e.AuthGeneration <= 0 || !deviceprotocol.ValidRequestID(e.RequestID) ||
		p.ReportedAt.IsZero() || p.ReceivedAt.IsZero() ||
		(p.Step == nil && p.Sleep == nil && p.SleepState == nil) ||
		p.Battery != nil || p.AppVersion != nil {
		return errors.New("invalid broker telemetry event")
	}
	if p.Step != nil && (*p.Step < 0 || *p.Step > 200000) ||
		p.Sleep != nil && (*p.Sleep < 0 || *p.Sleep > 24) ||
		p.SleepState != nil && *p.SleepState != "0" && *p.SleepState != "1" {
		return errors.New("invalid broker telemetry metric")
	}
	return nil
}

// SealTelemetry does not authenticate a device. The producer must obtain
// current binding and generation in its authorized transaction before calling.
func SealTelemetry(box *securestore.Box, event TelemetryEvent) ([]byte, error) {
	if err := event.validate(); err != nil {
		return nil, err
	}
	if box == nil {
		return nil, errors.New("missing event encryption key")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	sealed, err := box.Seal(string(data), brokerEventScope)
	return []byte(sealed), err
}

// MySQLSink uses the real Yihe resident/device locks and day/heartbeat
// repositories. Its additional receipt and generation tables belong only to
// the experimental schema; it is not installed by the production migrator.
type MySQLSink struct {
	DB  *sql.DB
	Box *securestore.Box
}

// Commit returns nil only after a committed mutation, a matching prior receipt,
// or a durable terminal rejection. Other errors leave the broker event unacked.
func (s MySQLSink) Commit(ctx context.Context, encoded []byte) error {
	if s.Box == nil {
		return errors.New("missing event encryption key")
	}
	plain, err := s.Box.Open(string(encoded), brokerEventScope)
	if err != nil {
		return err
	}
	var event TelemetryEvent
	if err := json.Unmarshal([]byte(plain), &event); err != nil {
		return err
	}
	if err := event.validate(); err != nil {
		return err
	}
	canonical, err := json.Marshal(event)
	if err != nil {
		return err
	}
	// Use the existing keyed index so low-entropy health metrics cannot be
	// guessed from an unkeyed receipt digest. Rotation must retain the lookup key.
	digest := s.Box.Index("yihe-next-telemetry-receipt-v1", string(canonical))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Follow the existing resident-first, device-second lock order.
	if err := devicerepo.LockResident(ctx, tx, event.ResidentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rejectEvent(ctx, tx, event, digest, "resident_missing")
		}
		return err
	}
	device, err := devicerepo.ScanDeviceRecord(devicerepo.LockedDeviceRow(ctx, tx, event.DeviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return rejectEvent(ctx, tx, event, digest, "device_missing")
	}
	if err != nil {
		return err
	}
	if device.OldID == nil || *device.OldID != event.ResidentID ||
		device.HealthEpoch != event.BindingEpoch || device.Status == nil || *device.Status == 0 {
		return rejectEvent(ctx, tx, event, digest, "binding_or_status")
	}
	var generation int64
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM broker_device_generation WHERE device_id=? FOR UPDATE", event.DeviceID).Scan(&generation); err != nil {
		// Missing generation is a schema/authorization error, not a successful ACK.
		return err
	}
	if generation != event.AuthGeneration {
		return rejectEvent(ctx, tx, event, digest, "authorization_generation")
	}

	// Terminal rejections stay terminal even if a future generation later
	// matches the rejected envelope.
	var reason string
	err = tx.QueryRowContext(ctx, "SELECT reason FROM broker_telemetry_rejection WHERE device_id=? AND binding_epoch=? AND request_id=? AND payload_digest=? FOR UPDATE",
		event.DeviceID, event.BindingEpoch, event.RequestID, digest).Scan(&reason)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var previous string
	err = tx.QueryRowContext(ctx, "SELECT payload_digest FROM broker_telemetry_receipt WHERE device_id=? AND binding_epoch=? AND request_id=? FOR UPDATE",
		event.DeviceID, event.BindingEpoch, event.RequestID).Scan(&previous)
	if err == nil {
		if previous != digest {
			return rejectEvent(ctx, tx, event, digest, "request_id_conflict")
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	p := event.Payload
	at := p.ReportedAt.UTC()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	if err := telemetryrepo.PersistDay(ctx, tx, device.ID, *device.OldID, device.HealthEpoch,
		day, p.ReportedAt, p.Step, p.Sleep, p.SleepState); err != nil {
		return err
	}
	if err := telemetryrepo.TouchDevice(ctx, tx, device.ID, device.HealthEpoch, nil, nil, p.ReceivedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO broker_telemetry_receipt(device_id,binding_epoch,request_id,payload_digest) VALUES(?,?,?,?)",
		event.DeviceID, event.BindingEpoch, event.RequestID, digest); err != nil {
		return err
	}
	return tx.Commit()
}

func rejectEvent(ctx context.Context, tx *sql.Tx, e TelemetryEvent, digest string, reason string) error {
	// No decrypted health data or device secrets in the rejection ledger.
	if _, err := tx.ExecContext(ctx, `INSERT INTO broker_telemetry_rejection(device_id,binding_epoch,request_id,payload_digest,reason)
		VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE reason=reason`,
		e.DeviceID, e.BindingEpoch, e.RequestID, digest, reason); err != nil {
		return err
	}
	return tx.Commit()
}

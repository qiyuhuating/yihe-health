package commandworkflow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/commandrepo"
	"yihe-care/internal/commands"
	"yihe-care/internal/db"
	"yihe-care/internal/devicerepo"
)

var (
	ErrDeviceMissing     = errors.New("device not found")
	ErrDeviceUnavailable = errors.New("device not bound and active")
	ErrKeyConflict       = errors.New("command request key reused")
	ErrProfileMissing    = errors.New("device command profile missing")
)

type InvalidAction struct{ Cause error }

func (e *InvalidAction) Error() string { return e.Cause.Error() }

type CreateRequest struct {
	DeviceID   int64
	Action     string
	RequestKey string
	TTL        int
}

type CreateResult struct {
	CommandID int64  `json:"commandId"`
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// Create owns the transaction from live authorization through command insert
// and audit commit. The locked device binding epoch fences command delivery.
func Create(ctx context.Context, store *db.Database, actor int64, req CreateRequest) (CreateResult, error) {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.IssueCommands)
	if err != nil {
		return CreateResult{}, err
	}
	defer tx.Rollback()

	device, err := devicerepo.ScanDeviceRecord(devicerepo.LockedDeviceRow(ctx, tx, req.DeviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, ErrDeviceMissing
	}
	if err != nil {
		return CreateResult{}, err
	}
	if device.OldID == nil || (device.Status != nil && *device.Status == 0) {
		return CreateResult{}, ErrDeviceUnavailable
	}

	prior, err := commandrepo.ByKey(ctx, tx, device.ID, device.HealthEpoch, req.RequestKey)
	if err == nil {
		if prior.Action != req.Action || prior.TTL != req.TTL {
			return CreateResult{}, ErrKeyConflict
		}
		if err := tx.Commit(); err != nil {
			return CreateResult{}, err
		}
		return CreateResult{CommandID: prior.ID, Status: prior.Status, Duplicate: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, err
	}

	wireType, err := commandrepo.WireType(ctx, tx, device.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, ErrProfileMissing
	}
	if err != nil {
		return CreateResult{}, err
	}
	channel, wire, err := commands.Resolve(wireType, req.Action)
	if err != nil {
		return CreateResult{}, &InvalidAction{Cause: err}
	}
	maxAttempts := 1
	if commands.RetrySafe(req.Action) {
		maxAttempts = 3
	}
	id, err := commandrepo.Insert(ctx, tx, commandrepo.Queued{
		DeviceID: device.ID, ResidentID: *device.OldID, Epoch: device.HealthEpoch,
		RequestKey: req.RequestKey, Action: req.Action, WireType: wireType,
		Channel: channel, WireAction: wire, MaxAttempts: maxAttempts,
		Now: time.Now().UTC(), CreatedBy: actor, TTL: req.TTL,
	})
	if err != nil {
		return CreateResult{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO care_access_audit(admin_id,action) VALUES(?,'device_command_create')", actor); err != nil {
		return CreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{CommandID: id, Status: "pending"}, nil
}

func List(ctx context.Context, store *db.Database, actor, deviceID, after int64) ([]commandrepo.Entry, error) {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ReadCommands)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := commandrepo.List(ctx, tx, deviceID, after)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

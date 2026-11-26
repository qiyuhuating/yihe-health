package deviceworkflow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/devicerepo"
)

const (
	CodeRequired    Failure = "BIND_CODE_REQUIRED"
	CodeNotFound    Failure = "BIND_CODE_NOT_FOUND"
	CodeUsed        Failure = "BIND_CODE_USED"
	CodeRace        Failure = "BIND_CODE_CONSUMED"
	CodeExpired     Failure = "BIND_CODE_EXPIRED"
	ResidentMissing Failure = "NOT_FOUND"
	AlreadyBound    Failure = "ALREADY_BOUND"
	BindFailed      Failure = "BIND_FAILED"
)

type BindInput struct {
	Number     string
	Secret     string
	Code       string
	ProfileDay time.Time
	At         time.Time
}

type BindResult struct {
	ResidentID int64
	Name       string
}

func Bind(ctx context.Context, store *db.Database, input BindInput) (BindResult, Failure, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return BindResult{}, "", err
	}
	defer tx.Rollback()

	device, failure, err := Authenticate(ctx, tx, input.Number, input.Secret)
	if err != nil || device == nil {
		return BindResult{}, failure, err
	}
	if input.Code == "" {
		return BindResult{}, CodeRequired, nil
	}
	code, err := devicerepo.FindBindCode(ctx, tx, input.Code)
	if err != nil {
		return BindResult{}, "", err
	}
	if failure = bindCodeFailure(code, time.Now()); failure != "" {
		return BindResult{}, failure, nil
	}
	resident, err := devicerepo.LockResidentBinding(ctx, tx, *code.OldID)
	if MissingRow(err) {
		return BindResult{}, ResidentMissing, nil
	}
	if err != nil {
		return BindResult{}, "", err
	}
	// Preserve the resident -> device lock order across binding and SOS.
	locked, err := devicerepo.ScanDeviceRecord(devicerepo.LockedDeviceRow(ctx, tx, device.ID))
	if err != nil {
		return BindResult{}, "", err
	}
	locked, failure, err = Verify(locked, input.Secret)
	if err != nil || locked == nil {
		return BindResult{}, failure, err
	}
	if locked.No != input.Number {
		return BindResult{}, InvalidCredential, nil
	}
	if locked.OldID != nil && *locked.OldID != *code.OldID {
		return BindResult{}, AlreadyBound, nil
	}
	err = devicerepo.CompleteBinding(ctx, tx, devicerepo.Binding{
		DeviceID: locked.ID, ResidentID: *code.OldID, FamilyID: code.FamilyID,
		CodeID: code.ID, ExistingDeviceID: resident.DeviceID, ExistingFamilyID: resident.FamilyID,
		At: input.At, ProfileDay: input.ProfileDay,
	})
	switch {
	case errors.Is(err, devicerepo.ErrBindFailed):
		return BindResult{}, BindFailed, nil
	case errors.Is(err, devicerepo.ErrBindCodeUsed):
		return BindResult{}, CodeRace, nil
	case err != nil:
		return BindResult{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return BindResult{}, "", err
	}
	result := BindResult{ResidentID: *code.OldID}
	if resident.Name != nil {
		result.Name = *resident.Name
	}
	return result, "", nil
}

func bindCodeFailure(code *devicerepo.BindCode, now time.Time) Failure {
	switch {
	case code == nil:
		return CodeNotFound
	case code.Used == 1:
		return CodeUsed
	case code.ExpireTime != nil && !code.ExpireTime.After(now):
		return CodeExpired
	case code.OldID == nil:
		return ResidentMissing
	default:
		return ""
	}
}

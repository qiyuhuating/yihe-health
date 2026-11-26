package deviceworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/devicecredential"
	"yihe-care/internal/devicerepo"
)

type Failure string

const (
	InvalidCredential Failure = "DEVICE_AUTH_FAILED"
	DeviceDisabled    Failure = "DEVICE_DISABLED"
	DeviceUnbound     Failure = "DEVICE_NOT_BOUND"
	BindingChanged    Failure = "BINDING_CHANGED"
	EpochExpired      Failure = "EPOCH_EXPIRED"
)

func MissingRow(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// Authenticate reads the stored device record without opening its encrypted coordinates.
func Authenticate(ctx context.Context, q db.RowQuerier, number, secret string) (*devicerepo.DeviceRecord, Failure, error) {
	device, err := devicerepo.FindDeviceByNo(ctx, q, number)
	if MissingRow(err) {
		return nil, InvalidCredential, nil
	}
	if err != nil {
		return nil, "", err
	}
	return Verify(device, secret)
}

func Verify(device *devicerepo.DeviceRecord, secret string) (*devicerepo.DeviceRecord, Failure, error) {
	if device == nil {
		return nil, InvalidCredential, nil
	}
	switch devicecredential.Check(secret, device.Secret, device.Status) {
	case devicecredential.InvalidSecret:
		return nil, InvalidCredential, nil
	case devicecredential.Disabled:
		return nil, DeviceDisabled, nil
	default:
		return device, "", nil
	}
}

// LockBoundDevice uses the same resident-then-device order for SOS and telemetry.
func LockBoundDevice(ctx context.Context, tx *sql.Tx, number, secret string, epoch int64) (*devicerepo.DeviceRecord, Failure, error) {
	device, failure, err := Authenticate(ctx, tx, number, secret)
	if err != nil || device == nil {
		return nil, failure, err
	}
	return LockAuthenticatedDevice(ctx, tx, device, number, secret, epoch)
}

// LockAuthenticatedDevice reuses an already verified, unlocked snapshot.
// The resident and device are still locked and revalidated before any write.
func LockAuthenticatedDevice(ctx context.Context, tx *sql.Tx, device *devicerepo.DeviceRecord, number, secret string, epoch int64) (*devicerepo.DeviceRecord, Failure, error) {
	if device == nil {
		return nil, InvalidCredential, nil
	}
	if device.OldID == nil {
		return nil, DeviceUnbound, nil
	}
	owner := *device.OldID
	if err := devicerepo.LockResident(ctx, tx, owner); err != nil {
		return nil, "", err
	}
	locked, err := devicerepo.ScanDeviceRecord(devicerepo.LockedDeviceRow(ctx, tx, device.ID))
	if err != nil {
		return nil, "", err
	}
	locked, failure, err := Verify(locked, secret)
	if err != nil || locked == nil {
		return nil, failure, err
	}
	if locked.No != number {
		return nil, InvalidCredential, nil
	}
	if epoch < locked.HealthEpoch {
		return nil, EpochExpired, nil
	}
	if locked.OldID == nil || *locked.OldID != owner || locked.HealthEpoch != epoch {
		return nil, BindingChanged, nil
	}
	return locked, "", nil
}

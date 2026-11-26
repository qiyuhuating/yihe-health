package devicerepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
)

// DeviceRecord is the stored device snapshot used by authentication and binding.
// Coordinates remain encrypted until the authorized caller opens them.
type DeviceRecord struct {
	ID            int64
	No            string
	Secret        string
	Alias         *string
	Model         *string
	AppVersion    *string
	Battery       *int
	OldID         *int64
	FamilyID      *int64
	Status        *int
	BindTime      *time.Time
	LastTime      *time.Time
	LastLng       *string
	LastLat       *string
	CreateTime    *time.Time
	PersonHandled int
	RateHandled   int
	PreHandled    int
	BootSeq       int
	HealthEpoch   int64
	HandledSeq    int
}

func ScanDeviceRecord(row *sql.Row) (*DeviceRecord, error) {
	var d DeviceRecord
	if err := row.Scan(&d.ID, &d.No, &d.Secret, &d.Alias, &d.Model, &d.AppVersion,
		&d.Battery, &d.OldID, &d.FamilyID, &d.Status, &d.BindTime, &d.LastTime,
		&d.LastLng, &d.LastLat, &d.CreateTime, &d.PersonHandled, &d.RateHandled,
		&d.PreHandled, &d.BootSeq, &d.HandledSeq, &d.HealthEpoch); err != nil {
		return nil, err
	}
	return &d, nil
}

func FindDeviceByNo(ctx context.Context, store db.RowQuerier, number string) (*DeviceRecord, error) {
	return ScanDeviceRecord(store.QueryRowContext(ctx, "SELECT "+deviceRecordColumns+" FROM device WHERE device_no=?", number))
}

func FindDeviceByID(ctx context.Context, store db.RowQuerier, id int64) (*DeviceRecord, error) {
	return ScanDeviceRecord(store.QueryRowContext(ctx, "SELECT "+deviceRecordColumns+" FROM device WHERE device_id=?", id))
}

package vitalrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"yihe-care/internal/securestore"
)

type Measurements struct {
	HeartRate *int64 `json:"hr"`
	Pressure  *int64 `json:"bp"`
}

type StoredMeasurements struct {
	Measurements
	Cipher *string
}

type protectedMeasurements struct {
	Owner  string `json:"owner"`
	Device string `json:"device"`
	Measurements
}

func MeasurementScope(id string) string { return "vital-measurements:" + id }

func measurementOwner(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

// Open keeps nullable legacy readings intact and verifies the original owner
// and device before returning protected values to an authorized reader.
func (stored StoredMeasurements) Open(box *securestore.Box, id int64, owner, device *int64) (Measurements, error) {
	if stored.Cipher == nil {
		return stored.Measurements, nil
	}
	if stored.HeartRate != nil || stored.Pressure != nil || len(*stored.Cipher) > 2048 {
		return Measurements{}, errors.New("invalid protected vital measurements")
	}
	plain, err := box.Open(*stored.Cipher, MeasurementScope(strconv.FormatInt(id, 10)))
	if err != nil {
		return Measurements{}, errors.New("cannot authenticate protected vital measurements")
	}
	var protected protectedMeasurements
	if json.Unmarshal([]byte(plain), &protected) != nil ||
		protected.Owner != measurementOwner(owner) || protected.Device != measurementOwner(device) ||
		(protected.HeartRate == nil && protected.Pressure == nil) {
		return Measurements{}, errors.New("unexpected protected vital measurements")
	}
	return protected.Measurements, nil
}

// StoreProtectedMeasurements seals both fields before clearing any legacy
// values. The caller owns the row lock and the encompassing transaction.
func StoreProtectedMeasurements(ctx context.Context, tx *sql.Tx, box *securestore.Box, id int64, owner, device *int64, values Measurements) error {
	if values.HeartRate == nil && values.Pressure == nil {
		return errors.New("no vital measurements to protect")
	}
	raw, err := json.Marshal(protectedMeasurements{Owner: measurementOwner(owner), Device: measurementOwner(device), Measurements: values})
	if err != nil {
		return err
	}
	cipher, err := box.Seal(string(raw), MeasurementScope(strconv.FormatInt(id, 10)))
	if err != nil {
		return errors.New("cannot seal vital measurements")
	}
	result, err := tx.ExecContext(ctx, "UPDATE ratepre SET hr=NULL,bp=NULL,measurement_cipher=? WHERE id=? AND measurement_cipher IS NULL",
		cipher, id)
	if err != nil {
		return errors.New("cannot save protected vital measurements")
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return errors.New("protected vital measurement write did not update one row")
	}
	return nil
}

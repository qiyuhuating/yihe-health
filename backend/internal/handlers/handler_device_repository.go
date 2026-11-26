package handlers

import (
	"context"
	"database/sql"
	"net/http"

	dbstore "yihe-care/internal/db"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/deviceworkflow"
	"yihe-care/internal/response"
)

func (h *Handler) readDeviceRecord(device *devicerepo.DeviceRecord, err error) (*devicerepo.DeviceRecord, error) {
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	device.LastLng, device.LastLat, err = openCoordinates(h.Box, device.LastLng, device.LastLat)
	if err != nil {
		return nil, err
	}
	return device, nil
}

// hwAuthIdentity authenticates a device without decrypting location data.
func (h *Handler) hwAuthIdentity(ctx context.Context, q dbstore.RowQuerier, deviceNo, secret, disabledMsg string) (*devicerepo.DeviceRecord, string, string, error) {
	device, failure, err := deviceworkflow.Authenticate(ctx, q, deviceNo, secret)
	if err != nil {
		return nil, "", "", err
	}
	switch failure {
	case deviceworkflow.InvalidCredential:
		return nil, "DEVICE_AUTH_FAILED", "设备号或密钥不正确", nil
	case deviceworkflow.DeviceDisabled:
		return nil, "DEVICE_DISABLED", disabledMsg, nil
	}
	return device, "", "", nil
}

// hwAuth also opens encrypted coordinates for endpoints that need them.
func (h *Handler) hwAuth(ctx context.Context, q dbstore.RowQuerier, deviceNo, secret, disabledMsg string) (*devicerepo.DeviceRecord, string, string, error) {
	device, code, message, err := h.hwAuthIdentity(ctx, q, deviceNo, secret, disabledMsg)
	if err != nil || device == nil {
		return device, code, message, err
	}
	device, err = h.readDeviceRecord(device, nil)
	return device, "", "", err
}

// lockDeviceID holds the device row lock until the caller finishes its transaction.
func lockDeviceID(w http.ResponseWriter, ctx context.Context, tx *sql.Tx, deviceID int64) bool {
	err := devicerepo.LockDeviceID(ctx, tx, deviceID)
	if isNotFound(err) {
		response.JSON(w, http.StatusNotFound, map[string]any{"message": "设备不存在"})
		return false
	}
	return !internalError(w, err)
}

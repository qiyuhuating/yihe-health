package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/deviceprotocol"
	"yihe-care/internal/devicerepo"

	"yihe-care/internal/deviceworkflow"
	"yihe-care/internal/response"
)

// POST /api/v1/device/sos: parse request and map the transactional outcome.
func (h *Handler) deviceSos(w http.ResponseWriter, r *http.Request) {
	r, _, signedOK := h.prepareSignedDeviceRequest(w, r)
	if !signedOK {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	req, ok := parseSOSRequest(w, m)
	if !ok {
		return
	}
	lng, _ := vstr(m, "lng")
	lat, _ := vstr(m, "lat")
	// Prepare request-only work before acquiring the resident/device locks.
	// Defer validation errors until after auth, idempotency and nonce checks.
	repo := telemetryRepository{box: h.Box}
	invalidPosition := checkPosition(lng, lat)
	errInvalidPosition := errors.New("invalid SOS position")
	var prepared preparedSOSPosition
	var meta positionMetadata
	var metaErr, prepareErr error
	if invalidPosition == "" {
		meta, metaErr = parsePositionMetadata(m, lng, lat, time.Now())
		if metaErr == nil {
			prepared, prepareErr = repo.prepareSOSPosition(lng, lat, meta)
		}
	}
	var prioritize func(context.Context, string, int64) (func(), error)
	if h.sosPriorityEnabled() {
		prioritize = h.healthWrites.prioritizeSOS
	}
	result, failure, err := deviceworkflow.SOS(r.Context(), h.DB.SOSPool(), h.Box, deviceworkflow.SOSInput{
		Number: req.deviceNo, Secret: req.secret, Epoch: req.bindingVersion, RequestID: req.requestID,
	}, deviceworkflow.SOSHooks{
		Prioritize: prioritize,
		AcceptNonce: func(ctx context.Context, tx *sql.Tx, deviceID int64) bool {
			return acceptSignedDeviceNonce(w, ctx, tx, deviceID)
		},
		Persist: func(ctx context.Context, tx *sql.Tx, device *devicerepo.DeviceRecord) (int64, error) {
			if invalidPosition != "" {
				return 0, errInvalidPosition
			}
			now := time.Now()
			// Preserve the original validation clock at the transaction boundary.
			// The normal valid path uses the already prepared ciphertext.
			if metaErr != nil || (meta.SampledAt != nil && meta.SampledAt.After(now.Add(5*time.Minute))) {
				meta, metaErr = parsePositionMetadata(m, lng, lat, now)
				if metaErr != nil {
					invalidPosition = metaErr.Error()
					return 0, errInvalidPosition
				}
				prepared, prepareErr = repo.prepareSOSPosition(lng, lat, meta)
			}
			if prepareErr != nil {
				return 0, prepareErr
			}
			return repo.PersistSOSIncident(ctx, tx, device, req.requestID, prepared, now)
		},
	})
	if failure == deviceworkflow.NonceRejected {
		return
	}
	if errors.Is(err, errInvalidPosition) {
		write400(w, invalidPosition)
		return
	}
	if internalError(w, err) {
		return
	}
	switch failure {
	case deviceworkflow.EpochExpired:
		response.Error(w, http.StatusConflict, string(failure), "绑定纪元已过期，请重新登录；原求助记录须保留核查")
		return
	case deviceworkflow.BindingChanged:
		response.Error(w, http.StatusConflict, string(failure), "绑定已变化，请重新登录；旧绑定求助不能归入新老人")
		return
	case deviceworkflow.SOSRequestConflict:
		conflict409(w, "求助编号已被其他事件或绑定使用，请检查设备请求编号")
		return
	case deviceworkflow.DeviceUnbound:
		hwFail(w, "DEVICE_NOT_BOUND", "设备还未绑定老人")
		return
	case deviceworkflow.DeviceDisabled:
		hwFail(w, "DEVICE_DISABLED", "设备已停用")
		return
	case deviceworkflow.InvalidCredential:
		hwFail(w, "DEVICE_AUTH_FAILED", "设备号或密钥不正确")
		return
	}
	if result.Duplicate {
		response.OK(w, map[string]any{
			"message":    "求助已接收（重复请求未再次入库）",
			"incidentId": result.IncidentID, "incidentState": result.IncidentState,
			"notificationState": result.NotificationState, "duplicate": true,
		})
		return
	}
	response.OK(w, map[string]any{
		"message":    "求助已接收，等待通知和人员接单",
		"incidentId": result.IncidentID, "incidentState": result.IncidentState,
		"notificationState": result.NotificationState,
	})
}

func (h *Handler) sosPriorityEnabled() bool {
	if h.Cfg.Requests.Loaded {
		return h.Cfg.Requests.SOSPriority
	}
	return config.Enabled("YIHE_SOS_PRIORITY")
}

type sosRequest struct {
	deviceNo, secret, requestID string
	bindingVersion              int64
}

func parseSOSRequest(w http.ResponseWriter, m map[string]any) (sosRequest, bool) {
	deviceNo, secret, ok := checkDeviceCredentials(w, m)
	if !ok {
		return sosRequest{}, false
	}
	if !checkTelemetry(w, m) {
		return sosRequest{}, false
	}
	bindingVersion, valid := int64Val(m, "bindingVersion")
	if !valid || bindingVersion < 1 {
		writeValidation400(w)
		return sosRequest{}, false
	}
	// SOS retry identifiers must be checked before opening a DB transaction.
	requestID, _ := vstr(m, "requestId")
	if !deviceprotocol.ValidRequestID(requestID) {
		writeValidation400(w)
		return sosRequest{}, false
	}
	return sosRequest{deviceNo: deviceNo, secret: secret, requestID: requestID, bindingVersion: bindingVersion}, true
}

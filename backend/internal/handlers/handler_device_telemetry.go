package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"yihe-care/internal/careworkflow"
	"yihe-care/internal/deviceprotocol"
	"yihe-care/internal/deviceworkflow"
	"yihe-care/internal/observability"
	"yihe-care/internal/response"
)

// POST /api/v1/device/report —— 设备数据上报（公开，事务）。
func (h *Handler) deviceReport(w http.ResponseWriter, r *http.Request) {
	r, signedOK := h.prepareSignedDeviceRequest(w, r)
	if !signedOK {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	deviceNo, secret, ok := checkDeviceCredentials(w, m)
	if !ok {
		return
	}
	if !checkTelemetry(w, m) {
		return
	}
	// Reject bad measurements and timestamps before taking a DB connection.
	parsed, ok := parseDeviceTelemetryPayload(w, m, time.Now())
	if !ok {
		return
	}
	isAlarm := parsed.alarm == "1" || strings.EqualFold(parsed.alarm, "fall")
	// Even ordinary measurements belong to a binding epoch. A supplied epoch
	// must be honored; silently ignoring it could attribute stale data to a
	// newly bound elder.
	bindingVersion, versionValid := int64Val(m, "bindingVersion")
	if _, supplied := m["bindingVersion"]; supplied && (!versionValid || bindingVersion < 1) {
		writeValidation400(w)
		return
	}
	var requestID string
	if isAlarm {
		requestID, _ = vstr(m, "requestId")
		if requestID != "" && !deviceprotocol.ValidRequestID(requestID) {
			writeValidation400(w)
			return
		}
		parsed.requestID = requestID
	}
	ctx := r.Context()
	// Authenticate before opening a transaction. The locked row is rechecked
	// below, so no owner-scoped writes can escape a concurrent bind or revoke.
	// The report needs no decrypted last-known coordinates.
	authAt := time.Now()
	d, authFailure, err := deviceworkflow.Authenticate(ctx, h.DB, deviceNo, secret)
	observability.Default.Add("yihe_device_report_auth_seconds_total", time.Since(authAt).Seconds())
	if internalError(w, err) {
		return
	}
	if d == nil {
		message := "设备号或密钥不正确"
		if authFailure == deviceworkflow.DeviceDisabled {
			message = "设备已停用"
		}
		hwFail(w, string(authFailure), message)
		return
	}
	if d.OldID == nil {
		hwFail(w, "DEVICE_NOT_BOUND", "设备还未绑定老人，请先在手表上输入绑定码")
		return
	}
	beginAt := time.Now()
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	observability.Default.Add("yihe_device_report_begin_seconds_total", time.Since(beginAt).Seconds())
	if internalError(w, err) {
		return
	}
	rollback := func() { _ = tx.Rollback() }
	// A report writes owner-scoped vitals, history and locations. Read-only
	// authentication above is not enough: unbind can commit between that read
	// and persistence. Take the same elder -> device locks as bind/SOS before
	// *any* owner-scoped write, including queued non-emergency telemetry.
	if !versionValid {
		if isAlarm && d.HealthEpoch != 1 {
			rollback()
			conflict409(w, "绑定版本已变化，请重新登录")
			return
		}
		// Preserve existing unversioned ordinary-report compatibility, but
		// recheck this snapshot's epoch under the row lock before writing.
		bindingVersion = d.HealthEpoch
	}
	lockAt := time.Now()
	locked, failure, lockErr := deviceworkflow.LockAuthenticatedDevice(ctx, tx, d, deviceNo, secret, bindingVersion)
	observability.Default.Add("yihe_device_report_binding_lock_seconds_total", time.Since(lockAt).Seconds())
	if rollbackOnError(w, rollback, lockErr) {
		return
	}
	if locked == nil {
		rollback()
		switch failure {
		case deviceworkflow.EpochExpired:
			response.Error(w, http.StatusConflict, string(failure), "绑定纪元已过期，请重新登录并清理旧纪元采样")
		case deviceworkflow.BindingChanged:
			response.Error(w, http.StatusConflict, string(failure), "绑定已变化，请重新登录；旧绑定数据不能归入新老人")
		case deviceworkflow.DeviceUnbound:
			hwFail(w, "DEVICE_NOT_BOUND", "设备还未绑定老人")
		case deviceworkflow.DeviceDisabled:
			hwFail(w, "DEVICE_DISABLED", "设备已停用")
		default:
			hwFail(w, "DEVICE_AUTH_FAILED", "设备号或密钥不正确")
		}
		return
	}
	d = locked
	// A numbered report is idempotent for the whole transaction: replaying the
	// same request returns the committed acknowledgement, while reusing the
	// number with different data is a hard conflict instead of silent data loss.
	receiptID, _ := vstr(m, "requestId")
	receiptDigest := ""
	// The durable queue already owns its replay contract (a unique key and
	// a 409 on collision); do not turn that into a duplicate acknowledgement.
	if receiptID != "" && deviceprotocol.ValidRequestID(receiptID) && !h.durableTelemetryEligible(parsed, receiptID, h.Box) {
		var digestErr error
		receiptDigest, digestErr = reportPayloadDigest(m)
		if digestErr != nil {
			rollback()
			writeValidation400(w)
			return
		}
		priorDigest, priorResult, found, lookupErr := lookupDeviceReportReceipt(ctx, tx, d.ID, d.HealthEpoch, receiptID)
		if lookupErr != nil {
			rollback()
			response.Error500(w, ref())
			return
		}
		if found {
			rollback()
			if priorDigest != receiptDigest {
				conflict409(w, "同一请求编号对应不同上报内容")
				return
			}
			var fields map[string]any
			if json.Unmarshal([]byte(priorResult), &fields) != nil {
				response.Error500(w, ref())
				return
			}
			fields["duplicate"] = true
			response.OK(w, fields)
			return
		}
	}
	if isAlarm {
		if requestID != "" {
			prior, err := careworkflow.FindReportRetry(ctx, tx, d.ID, requestID)
			if err != nil {
				rollback()
				response.Error500(w, ref())
				return
			}
			if prior != nil {
				rollback()
				kind := "device_alarm"
				if strings.EqualFold(parsed.alarm, "fall") {
					kind = "fall"
				}
				if prior.ResidentID != *d.OldID || prior.Kind != kind {
					conflict409(w, "设备上报编号已被其他事件或绑定使用")
					return
				}
				response.OK(w, map[string]any{
					"incidentId": prior.ID, "incidentState": prior.State,
					"duplicate": true, "message": "设备告警已接收（重复请求）",
				})
				return
			}
		}
	}
	if !acceptSignedDeviceNonce(w, ctx, tx, d.ID) {
		rollback()
		return
	}
	if requestID, present := vstr(m, "requestId"); present &&
		h.durableTelemetryEligible(parsed, requestID, h.Box) {
		err := (telemetryRepository{box: h.Box}).QueueDurableReport(ctx, tx, d, parsed, requestID)
		if err != nil {
			rollback()
			if durableQueueDuplicate(err) {
				conflict409(w, "设备上报编号已存在，请勿重复生成")
			} else {
				response.Error500(w, ref())
			}
			return
		}
		queuedResult := map[string]any{
			"rateState": nil, "preState": nil, "queued": true,
			"message":    "设备数据已持久接收，等待后台处理",
			"serverTime": parsed.now.Format(tsLayout),
		}
		if !stageDeviceReportReceipt(w, ctx, tx, rollback, d, receiptID, receiptDigest, queuedResult) {
			return
		}
		if internalError(w, tx.Commit()) {
			return
		}
		response.OK(w, queuedResult)
		return
	}
	persistAt := time.Now()
	persisted, err := (telemetryRepository{box: h.Box}).PersistDeviceReport(ctx, tx, d, parsed)
	observability.Default.Add("yihe_device_report_persist_seconds_total", time.Since(persistAt).Seconds())
	if rollbackOnError(w, rollback, err) {
		return
	}
	result := map[string]any{
		"rateState": persisted.rateState, "preState": persisted.preState,
		"serverTime": parsed.now.Format(tsLayout),
	}
	if persisted.careIncidentID != 0 {
		result["incidentId"] = persisted.careIncidentID
		result["incidentState"] = "received"
	}
	if !stageDeviceReportReceipt(w, ctx, tx, rollback, d, receiptID, receiptDigest, result) {
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, result)
}

package handlers

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"yihe-care/internal/devicerepo"
	"yihe-care/internal/deviceworkflow"
	"yihe-care/internal/response"
	"yihe-care/internal/telemetryrepo"
)

func checkDeviceCredentials(w http.ResponseWriter, m map[string]any) (string, string, bool) {
	deviceNo, okNo := vstr(m, "deviceNo")
	secret, okSecret := vstr(m, "secret")
	if !okNo || deviceNo == "" || utf8.RuneCountInString(deviceNo) > 32 ||
		!okSecret || secret == "" || utf8.RuneCountInString(secret) > 128 {
		writeValidation400(w)
		return "", "", false
	}
	return strings.ToUpper(deviceNo), secret, true
}

// hwFail writes the device domain error with its HTTP status.
func hwFail(w http.ResponseWriter, code, msg string) {
	status := http.StatusBadRequest
	switch code {
	case "DEVICE_AUTH_FAILED":
		status = http.StatusUnauthorized
	case "DEVICE_DISABLED":
		status = http.StatusForbidden
	case "DEVICE_NOT_BOUND", "BIND_CODE_USED", "BIND_CODE_EXPIRED":
		status = http.StatusConflict
	case "BIND_CODE_NOT_FOUND", "NOT_FOUND":
		status = http.StatusNotFound
	}
	response.Error(w, status, code, msg)
}

// POST /api/v1/device/login —— 设备登录（公开；限流由全局中间件处理）。
func (h *Handler) deviceLogin(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	deviceNo, secret, ok := checkDeviceCredentials(w, m)
	if !ok {
		return
	}
	harmony := false
	if platform, present := m["platform"]; present {
		version, valid := int64Val(m, "protocolVersion")
		if platform != "harmonyos" || !valid || version != 1 {
			write400(w, "不支持的手表平台或协议版本；HarmonyOS 须使用协议 1")
			return
		}
		harmony = true
	}
	ctx := r.Context()
	d, code, msg, err := h.hwAuthIdentity(ctx, h.DB, deviceNo, secret, "设备已停用，请联系管理员")
	if internalError(w, err) {
		return
	}
	if d == nil {
		hwFail(w, code, msg)
		return
	}
	now := time.Now()
	if internalError(w, telemetryrepo.TouchDevice(ctx, h.DB, d.ID, d.HealthEpoch, nil, nil, now)) {
		return
	}
	bound := 0
	fields := map[string]any{
		"deviceId": d.ID, "deviceNo": d.No, "alias": d.Alias,
		"bound": bound, "residentId": d.OldID, "bindingVersion": d.HealthEpoch,
	}
	if harmony {
		fields["protocolVersion"] = 1
		fields["maxBatchSize"] = 200
	}
	if d.OldID != nil {
		bound = 1
		fields["bound"] = bound
		residentName, err := devicerepo.ResidentName(ctx, h.DB, *d.OldID)
		if err != nil && !isNotFound(err) {
			response.Error500(w, ref())
			return
		}
		if residentName != nil {
			fields["residentName"] = *residentName
		} else {
			fields["residentName"] = ""
		}
	}
	response.OK(w, fields)
}

// POST /api/v1/device/bind: device binding owns its database transaction.
func (h *Handler) deviceBind(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	deviceNo, secret, ok := checkDeviceCredentials(w, m)
	if !ok {
		return
	}
	codeRaw, _ := vstr(m, "code")
	if utf8.RuneCountInString(codeRaw) > 20 {
		writeValidation400(w)
		return
	}
	now := time.Now()
	result, failure, err := deviceworkflow.Bind(r.Context(), h.DB, deviceworkflow.BindInput{
		Number: deviceNo, Secret: secret, Code: normalizeCode(codeRaw),
		At: now, ProfileDay: siteDay(now),
	})
	if internalError(w, err) {
		return
	}
	switch failure {
	case "":
		response.OK(w, map[string]any{"residentId": result.ResidentID, "residentName": result.Name})
	case deviceworkflow.AlreadyBound:
		conflict409(w, "设备已绑定其他长者，请先在设备管理解除绑定")
	case deviceworkflow.BindFailed:
		conflict409(w, "设备绑定失败")
	case deviceworkflow.CodeUsed:
		hwFail(w, "BIND_CODE_USED", "该绑定码已被使用")
	case deviceworkflow.CodeRace:
		conflict409(w, "该绑定码已被使用")
	case deviceworkflow.CodeRequired:
		hwFail(w, "BIND_CODE_REQUIRED", "请输入绑定码")
	case deviceworkflow.CodeNotFound:
		hwFail(w, "BIND_CODE_NOT_FOUND", "绑定码不存在，请在系统里重新生成")
	case deviceworkflow.CodeExpired:
		hwFail(w, "BIND_CODE_EXPIRED", "该绑定码已过期，请重新生成")
	case deviceworkflow.ResidentMissing:
		hwFail(w, "NOT_FOUND", "绑定码对应的老人不存在")
	case deviceworkflow.DeviceDisabled:
		hwFail(w, "DEVICE_DISABLED", "设备已停用")
	default:
		hwFail(w, "DEVICE_AUTH_FAILED", "设备号或密钥不正确")
	}
}

// POST /api/v1/device/heartbeat —— 设备心跳（公开）。
func (h *Handler) deviceHeartbeat(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	deviceNo, secret, ok := checkDeviceCredentials(w, m)
	if !ok {
		return
	}
	battery, emsg := parseMetric(m, "battery", 0, 100)
	if emsg != "" {
		write400(w, emsg)
		return
	}
	appVersion, emsg := parseText(m, "appVersion", 20)
	if emsg != "" {
		write400(w, emsg)
		return
	}
	ctx := r.Context()
	d, code, msg, err := h.hwAuthIdentity(ctx, h.DB, deviceNo, secret, "设备已停用")
	if internalError(w, err) {
		return
	}
	if d == nil {
		hwFail(w, code, msg)
		return
	}
	now := time.Now()
	if h.Heartbeats != nil {
		if internalError(w, h.Heartbeats.Record(ctx, d.ID, d.HealthEpoch, battery, appVersion, now)) {
			return
		}
	} else if internalError(w, telemetryrepo.TouchDevice(ctx, h.DB, d.ID, d.HealthEpoch, battery, appVersion, now)) {
		return
	}
	bound := 0
	if d.OldID != nil {
		bound = 1
	}
	response.OK(w, map[string]any{
		"serverTime": now.Format(tsLayout), "bound": bound, "residentId": d.OldID, "bindingVersion": d.HealthEpoch,
		"reportInterval": 5, "pendingAlarm": 0,
	})
}

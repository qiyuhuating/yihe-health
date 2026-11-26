package handlers

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"yihe-care/internal/carepolicy"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/residentprofile"
	"yihe-care/internal/response"
	"yihe-care/internal/securecode"
)

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// intOr400 body 整数字段校验（device/list 等用 @Valid 风格：失败一律通用 400 文案）。
func intOr400(w http.ResponseWriter, m map[string]any, key string, def, min, max int) (int, bool) {
	v, present := m[key]
	if !present || v == nil {
		return def, true
	}
	var raw string
	switch t := v.(type) {
	case float64:
		if math.Trunc(t) != t {
			writeValidation400(w)
			return 0, false
		}
		raw = strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return def, true
		}
		raw = s
	default:
		writeValidation400(w)
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		writeValidation400(w)
		return 0, false
	}
	return n, true
}

// POST /api/v1/device/list —— 设备列表（ADMIN，分页+在线状态）。
func (h *Handler) deviceList(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMapOrEmpty(w, r)
	if !ok {
		return
	}
	p, ok := intOr400(w, m, "page", 1, 1, 100000)
	if !ok {
		return
	}
	ps, ok := intOr400(w, m, "pageSize", 20, 1, 100)
	if !ok {
		return
	}
	query, _ := vstr(m, "query")
	query = strings.ToLower(query)
	if utf8.RuneCountInString(query) > 100 {
		writeValidation400(w)
		return
	}
	state, _ := vstr(m, "state")
	if state == "" {
		state = "all"
	}
	if state != "all" && state != "online" && state != "offline" {
		writeValidation400(w)
		return
	}
	pattern := ""
	if query != "" {
		pattern = "%" + query + "%"
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadDevices)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	if internalError(w, residentprofile.VerifyKey(ctx, tx, h.Box)) {
		return
	}
	items, total, onlineCount, err := devicerepo.ListAdminDevices(ctx, tx, pattern, state, p, ps)
	if internalError(w, err) {
		return
	}
	now := time.Now()
	list := make([]map[string]any, 0, len(items))
	for _, item := range items {
		view, err := h.adminDeviceView(item, now)
		if internalError(w, err) {
			return
		}
		list = append(list, view)
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{
		"total": total, "onlineCount": onlineCount, "page": p, "pageSize": ps,
		"devices": list,
	})
}

// toIntRanged returns zero for invalid or out-of-range values.
func toIntRanged(s string, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < min || n > max {
		return 0
	}
	return n
}

// genBindCode 生成唯一 6 位绑定码（最多重试 30 次）。
func (h *Handler) genBindCode(ctx context.Context, q dbstore.RowQuerier) (string, error) {
	for i := 0; i < 30; i++ {
		c, err := securecode.Generate(6)
		if err != nil {
			return "", err
		}
		exists, err := devicerepo.CodeExists(ctx, q, c)
		if err != nil {
			return "", err
		}
		if !exists {
			return c, nil
		}
	}
	return "", errors.New("无法生成唯一绑定码")
}

// POST /api/v1/device/createBindCode —— 生成绑定码（ADMIN）。
func (h *Handler) deviceCreateBindCode(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	oldIDStr, present := vstr(m, "oldId")
	if !present || !validID(oldIDStr) {
		writeValidation400(w)
		return
	}
	expireDaysStr, _ := vstr(m, "expireDays")
	if utf8.RuneCountInString(expireDaysStr) > 3 {
		writeValidation400(w)
		return
	}
	expireDays := 1
	if expireDaysStr != "" {
		expireDays = toIntRanged(expireDaysStr, 1, 30)
		if expireDays == 0 {
			write400(w, "有效天数必须为 1 至 30 的整数")
			return
		}
	}
	oldID, _ := strconv.ParseInt(oldIDStr, 10, 64)
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageDevices)
	if !ok {
		return
	}
	defer tx.Rollback()
	ctx := r.Context()
	oldName, err := devicerepo.ResidentName(ctx, tx, oldID)
	if err != nil {
		if isNotFound(err) {
			response.Fail(w, "老人不存在")
		} else {
			response.Error500(w, ref())
		}
		return
	}
	createUser := creatorName(r)
	now := time.Now()
	expireTime := now.AddDate(0, 0, expireDays)
	code, err := h.genBindCode(ctx, tx)
	if internalError(w, err) {
		return
	}
	id, err := devicerepo.CreateBindCode(ctx, tx, code, oldID, createUser, now, expireTime)
	if err != nil {
		if duplicateConflict(w, err, "绑定码生成冲突，请重试") {
			return
		}
		response.Error500(w, ref())
		return
	}
	if id == 0 {
		response.Fail(w, "生成绑定码失败")
		return
	}
	nm := ""
	if oldName != nil {
		nm = *oldName
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{
		"code": code, "oldId": oldID, "oldPersonName": nm,
		"expireTime": expireTime.Format(tsLayout),
	})
}

// POST /api/v1/device/bindCodeList —— 某老人的绑定码列表（ADMIN）。
func (h *Handler) deviceBindCodeList(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	oldIDStr, present := vstr(m, "oldId")
	if !present || oldIDStr == "" {
		writeValidation400(w)
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadDevices)
	if !ok {
		return
	}
	defer tx.Rollback()
	list := []map[string]any{}
	// 非数字 → 返回空列表而非报错。
	if validID(oldIDStr) {
		oldID, _ := strconv.ParseInt(oldIDStr, 10, 64)
		codes, err := devicerepo.ListBindCodes(r.Context(), tx, oldID)
		if internalError(w, err) {
			return
		}
		for _, code := range codes {
			list = append(list, map[string]any{
				"id": code.ID, "code": code.Code, "expireTime": tsOrNil(code.ExpireTime),
				"used": code.Used, "usedTime": tsOrNil(code.UsedTime),
				"usedDeviceId": code.UsedDeviceID,
			})
		}
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"bindCodes": list})
}

// POST /api/v1/device/deleteBindCode —— 作废未使用的绑定码（ADMIN）。
func (h *Handler) deviceDeleteBindCode(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	id, ok := requireID(w, m, "id")
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageDevices)
	if !ok {
		return
	}
	defer tx.Rollback()
	deleted, err := devicerepo.DeleteUnusedBindCode(r.Context(), tx, id)
	if internalError(w, err) {
		return
	}
	if deleted {
		if internalError(w, tx.Commit()) {
			return
		}
		response.OK(w, nil)
		return
	}
	response.Fail(w, "绑定码已被使用或不存在")
}

// genDeviceNo 生成唯一设备号（W + 8 位，重试 30 次，兜底时间戳）。
func (h *Handler) adminDeviceView(device devicerepo.AdminDevice, now time.Time) (map[string]any, error) {
	if device.ResidentID == nil && (device.ResidentProfile.Protected || device.ResidentProfile.Cipher != nil || device.ResidentProfile.Lookup != nil) {
		return nil, errors.New("protected device resident profile has no identity")
	}
	if device.ResidentID != nil {
		device.ResidentProfile.Legacy.Phone = device.ResidentPhone
		values, err := device.ResidentProfile.Open(h.Box, *device.ResidentID)
		if err != nil {
			return nil, err
		}
		device.ResidentPhone = values.Phone
	}
	name := "（未绑定）"
	if device.ResidentName != nil {
		name = *device.ResidentName
	}
	online := 0
	if device.LastTime != nil && now.Sub(*device.LastTime) <= 60*time.Second {
		online = 1
	}
	lastLng, lastLat, err := openCoordinates(h.Box, device.LastLongitude, device.LastLatitude)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"deviceId": device.ID, "deviceNo": device.Number, "alias": device.Alias,
		"model": device.Model, "appVersion": device.AppVersion,
		"battery": device.Battery, "oldId": device.ResidentID,
		"oldPersonName": name, "oldPersonPhone": device.ResidentPhone,
		"bindTime": tsOrNil(device.BoundAt), "lastTime": tsOrNil(device.LastTime),
		"lastLng": lastLng, "lastLat": lastLat, "status": device.Status,
		"online": online, "personHandled": device.PersonHandled,
		"rateHandled": device.RateHandled, "preHandled": device.PressureHandled,
	}, nil
}

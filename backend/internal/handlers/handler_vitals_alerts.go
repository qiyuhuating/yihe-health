package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/response"
	"yihe-care/internal/vitalpolicy"
	"yihe-care/internal/vitalrepo"
)

type vitalRecord struct {
	Name        *string `json:"name"`
	OldID       *int64  `json:"oldId"`
	Sex         *string `json:"sex"`
	RateState   *string `json:"rateState"`
	PreState    *string `json:"preState"`
	Hr          *int64  `json:"hr"`
	Bp          *int64  `json:"bp"`
	RecordID    *int64  `json:"recordId"`
	RateHandled *string `json:"rateHandled"`
	PreHandled  *string `json:"preHandled"`
	CreateTime  *string `json:"createTime"`
}

const vitalRecordSelect = `SELECT o.name AS name, o.old_id AS oldId, o.sex AS sex,` +
	` r.rate_state AS rateState, r.pre_state AS preState, r.hr AS hr, r.bp AS bp, r.id AS recordId, r.measurement_cipher, r.device_id,` +
	` CAST(r.rate_handled AS CHAR) AS rateHandled, CAST(r.pre_handled AS CHAR) AS preHandled,` +
	` DATE_FORMAT(r.create_time,'%Y-%m-%d %H:%i:%s') AS createTime` +
	` FROM oldperson o`

// 按长者选出最新的心率和血压记录。
const latestVitalsQuery = vitalRecordSelect +
	` INNER JOIN (SELECT *, ROW_NUMBER() OVER (PARTITION BY old_id ORDER BY create_time DESC, id DESC) AS rn FROM ratepre) r` +
	` ON r.old_id=o.old_id AND r.rn=1`

func (h *Handler) scanVitalRecord(rows *sql.Rows) (vitalRecord, error) {
	var d vitalRecord
	var id int64
	var deviceID *int64
	var stored vitalrepo.StoredMeasurements
	if err := rows.Scan(&d.Name, &d.OldID, &d.Sex, &d.RateState, &d.PreState, &stored.HeartRate, &stored.Pressure,
		&id, &stored.Cipher, &deviceID, &d.RateHandled, &d.PreHandled, &d.CreateTime); err != nil {
		return d, err
	}
	values, err := stored.Open(h.Box, id, d.OldID, deviceID)
	d.RecordID, d.Hr, d.Bp = &id, values.HeartRate, values.Pressure
	return d, err
}

// POST /api/v1/vitals/alerts/count —— 未处置的心率/血压异常数量。
func (h *Handler) countVitalAlerts(w http.ResponseWriter, r *http.Request) {
	if _, ok := optBody(w, r); !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadAlerts)
	if !ok {
		return
	}
	defer tx.Rollback()
	rate, pressure, err := vitalrepo.AlertCounts(r.Context(), tx)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"rateTotal": rate, "preTotal": pressure, "total": rate + pressure})
}

// POST /api/v1/vitals/alerts/detail —— 查看一条异常记录的原因。
func (h *Handler) getVitalAlert(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	recordIDStr := strID(m, "recordId")
	oldIDStr := strID(m, "oldId")
	kind := strings.ToLower(str(m, "kind"))

	principal, ok := principalOr401(w, r)
	if !ok {
		return
	}
	var tx *sql.Tx
	var familyOldID int64
	if principal.Role == auth.RoleFamily {
		switch {
		case isDigits(oldIDStr):
			familyOldID, _ = strconv.ParseInt(oldIDStr, 10, 64)
		case isDigits(recordIDStr):
			recordID, _ := strconv.ParseInt(recordIDStr, 10, 64)
			var owner sql.NullInt64
			err := h.DB.QueryRowContext(r.Context(), "SELECT old_id FROM ratepre WHERE id=?", recordID).Scan(&owner)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && !owner.Valid) {
				response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED"})
				return
			}
			if internalError(w, err) {
				return
			}
			familyOldID = owner.Int64
		default:
			response.Fail(w, "参数不正确")
			return
		}
		tx, ok = h.beginScopedHealthRead(w, r, familyOldID)
	} else {
		tx, ok = h.beginAdminActionTx(w, r, carepolicy.ReadAlerts)
	}
	if !ok {
		return
	}
	defer tx.Rollback()
	var a vitalrepo.Alert
	var found bool
	var err error
	if isDigits(recordIDStr) {
		id, _ := strconv.ParseInt(recordIDStr, 10, 64)
		a, found, err = vitalrepo.FindAlert(r.Context(), tx, h.Box, id)
	} else if isDigits(oldIDStr) {
		id, _ := strconv.ParseInt(oldIDStr, 10, 64)
		a, found, err = vitalrepo.FindLatestPending(r.Context(), tx, h.Box, id, kind)
	} else {
		response.Fail(w, "参数不正确")
		return
	}
	if internalError(w, err) {
		return
	}
	if principal.Role == auth.RoleFamily && found && (a.OldID == nil || *a.OldID != familyOldID) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED"})
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	if !found {
		response.OK(w, map[string]any{"hasAlert": 0, "message": "该老人当前没有待处理的心率/血压异常"})
		return
	}
	response.OK(w, vitalpolicy.DescribeRatepreAlert(a))
}

// POST /api/v1/vitals/alerts/resolve —— 处置异常（消除红点），事务内执行。
func (h *Handler) handleResolveVitalAlert(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	recordIDStr := strID(m, "recordId")
	kind := strings.ToLower(str(m, "kind"))
	if raw, present := m["kind"]; present && raw != nil {
		if _, isString := raw.(string); !isString || (kind != "" && kind != "rate" && kind != "pre") {
			write400(w, "kind 应为 rate、pre 或空字符串")
			return
		}
	}
	if !isDigits(recordIDStr) {
		response.Fail(w, "参数不正确")
		return
	}
	recordID, _ := strconv.ParseInt(recordIDStr, 10, 64)
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.HandleAlerts)
	if !ok {
		return
	}
	defer tx.Rollback()
	changed, err := vitalrepo.ResolveAlert(r.Context(), tx, recordID, kind)
	if errors.Is(err, vitalrepo.ErrAlertNotFound) {
		response.Fail(w, "记录不存在")
		return
	}
	if internalError(w, err) {
		return
	}
	if !changed {
		recordNotFound(w)
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"message": "已处置，红点已消除"})
}

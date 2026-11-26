package handlers

import (
	"database/sql"
	"net/http"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/response"
	"yihe-care/internal/vitalrepo"
)

// POST /api/v1/vitals/latest —— 按姓名+时间范围分页查询。
func (h *Handler) listLatestVitals(w http.ResponseWriter, r *http.Request) {
	listLatestMeasurements(h, w, r, latestVitalsQuery, "vitalRecords", h.scanVitalRecord)
}

// POST /api/v1/vitals/history —— 家属查询自己老人的心率血压列表。
func (h *Handler) listVitalHistory(w http.ResponseWriter, r *http.Request) {
	oldID, ok := h.readScopedOldID(w, r)
	if !ok {
		return
	}
	sql := vitalRecordSelect +
		` INNER JOIN (SELECT * FROM ratepre WHERE old_id=? ORDER BY create_time DESC, id DESC LIMIT 1) r ON r.old_id=o.old_id` +
		` ORDER BY createTime DESC`
	tx, ok := h.beginScopedHealthRead(w, r, oldID)
	if !ok {
		return
	}
	defer tx.Rollback()
	items, err := dbstore.QueryList(r.Context(), tx, h.scanVitalRecord, sql, oldID)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"vitalRecords": items})
}

// POST /api/v1/vitals/chart —— 家属查询老人心率血压明细（图表用）。
func (h *Handler) getVitalChart(w http.ResponseWriter, r *http.Request) {
	oldID, ok := h.readScopedOldID(w, r)
	if !ok {
		return
	}
	tx, ok := h.beginScopedHealthRead(w, r, oldID)
	if !ok {
		return
	}
	defer tx.Rollback()
	// The query returns at most 30 points, already in display order.
	hrs := make([]*int64, 0, 30)
	bps := make([]*int64, 0, 30)
	createTimes := make([]string, 0, 30)
	var timeStr, lastDay string
	err := dbstore.QueryEach(r.Context(), tx, func(rows *sql.Rows) error {
		var id int64
		var device *int64
		var createTime *string
		var stored vitalrepo.StoredMeasurements
		if err := rows.Scan(&id, &device, &stored.HeartRate, &stored.Pressure, &stored.Cipher, &createTime); err != nil {
			return err
		}
		values, err := stored.Open(h.Box, id, &oldID, device)
		if err != nil {
			return err
		}
		ct := ""
		if createTime != nil {
			ct = *createTime
		}
		if timeStr == "" {
			timeStr = safeSub(ct, 0, 10)
		}
		if ct != "" {
			lastDay = safeSub(ct, 0, 10)
		}
		hrs = append(hrs, values.HeartRate)
		bps = append(bps, values.Pressure)
		createTimes = append(createTimes, ct)
		return nil
	}, `SELECT r.id, r.device_id, r.hr, r.bp, r.measurement_cipher, DATE_FORMAT(r.create_time,'%Y-%m-%d %H:%i:%s') AS createTime
		FROM (SELECT id, device_id, hr, bp, measurement_cipher, create_time FROM ratepre WHERE old_id=? ORDER BY create_time DESC, id DESC LIMIT 30) r
		ORDER BY r.create_time ASC, r.id ASC`, oldID)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	if lastDay != "" && lastDay != timeStr {
		timeStr += " 至 " + lastDay
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"time":        timeStr,
		"hrs":         hrs,
		"bps":         bps,
		"createTimes": createTimes,
	})
}

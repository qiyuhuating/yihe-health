package handlers

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/fence"
	"yihe-care/internal/fencerepo"
	"yihe-care/internal/response"
	"yihe-care/internal/telemetryrepo"
)

func (h *Handler) fenceSave(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	var req fencerepo.Config
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.ID < 0 || req.DeviceID < 1 || req.Revision < 0 || strings.TrimSpace(req.Name) == "" || utf8.RuneCountInString(req.Name) > 64 || req.CoordinateSystem != "WGS84" || req.TimeBegin < 0 || req.TimeBegin >= 86400 || req.TimeEnd <= 0 || req.TimeEnd > 86400 || req.TimeBegin == req.TimeEnd || req.MinPoints < 2 || req.MinPoints > 20 || req.DwellSeconds < 1 || req.DwellSeconds > 3600 || math.IsNaN(req.MaxAccuracy) || req.MaxAccuracy <= 0 || req.MaxAccuracy > 1000 {
		write400(w, "围栏配置无效；须明确 WGS84、时区、时段、精度与去抖参数")
		return
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil || req.Timezone == "" || req.Timezone == "Local" {
		write400(w, "须使用明确的 IANA 时区")
		return
	}
	if err := fence.Validate(req.Coordinates); err != nil {
		write400(w, err.Error())
		return
	}
	ctx := r.Context()
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageGeofences)
	if !ok {
		return
	}
	defer tx.Rollback()
	d, err := devicerepo.ScanDeviceRecord(devicerepo.LockedDeviceRow(ctx, tx, req.DeviceID))
	if isNotFound(err) {
		response.JSON(w, 404, map[string]any{"message": "设备不存在"})
		return
	}
	if internalError(w, err) {
		return
	}
	if d.OldID == nil {
		conflict409(w, "设备尚未绑定老人")
		return
	}
	err = fencerepo.Save(ctx, tx, actor, d.ID, *d.OldID, d.HealthEpoch, &req)
	switch {
	case errors.Is(err, fencerepo.ErrFenceLimit):
		conflict409(w, "每个绑定最多 20 个围栏，请编辑或停用已有围栏")
		return
	case errors.Is(err, fencerepo.ErrFenceRevision):
		conflict409(w, "围栏或绑定版本已变化")
		return
	case internalError(w, err):
		return
	}
	err = commitCareChange(ctx, tx, actor, "fence_save")
	if internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"id": req.ID, "revision": req.Revision, "state": "saved", "deviceApplied": false})
}

func (h *Handler) fenceList(w http.ResponseWriter, r *http.Request) {
	if _, ok := careAdmin(w, r); !ok {
		return
	}
	var req struct {
		DeviceID int64 `json:"deviceId"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.DeviceID < 1 {
		write400(w, "设备无效")
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadGeofences)
	if !ok {
		return
	}
	defer tx.Rollback()
	items, err := fencerepo.ListConfigured(r.Context(), tx, req.DeviceID)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"items": items})
}

func (h *Handler) fencePoint(w http.ResponseWriter, r *http.Request) {
	var req fencePointRequest
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.CoordinateSystem != "WGS84" || req.Point == nil || !fence.ValidPoint(*req.Point) || req.Accuracy <= 0 || req.Accuracy > 1000 || math.IsNaN(req.Accuracy) || time.Since(req.OccurredAt) > 5*time.Minute || time.Until(req.OccurredAt) > time.Minute {
		write400(w, "须提供明确的 WGS84 定位、精度和最近 5 分钟内的时间")
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if internalError(w, err) {
		return
	}
	defer tx.Rollback()
	d := h.capabilityDevice(w, r, tx, req.capabilityCredentials)
	if d == nil {
		return
	}
	configs, err := fencerepo.ListActive(ctx, tx, d.ID, *d.OldID, d.HealthEpoch)
	if internalError(w, err) {
		return
	}
	events := []map[string]any{}
	for _, f := range configs {
		state, err := fencerepo.LoadState(ctx, tx, f.ID, f.Revision)
		if internalError(w, err) {
			return
		}
		at := req.OccurredAt.UTC().Truncate(time.Microsecond)
		direction, fresh, err := advanceFenceObservation(&f, &state, &req, at)
		if internalError(w, err) {
			return
		}
		if !fresh {
			continue
		}
		if internalError(w, fencerepo.SaveState(ctx, tx, f.ID, f.Revision, state)) {
			return
		}
		if direction == "" {
			continue
		}
		event, err := h.persistFenceEvent(ctx, tx, d, &f, &req, direction, at)
		if internalError(w, err) {
			return
		}
		events = append(events, event)
	}
	if err = telemetryrepo.TouchDevice(ctx, tx, d.ID, d.HealthEpoch, nil, nil, time.Now()); internalError(w, err) {
		return
	}
	if err = tx.Commit(); internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"events": events})
}

func (h *Handler) fenceEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := careAdmin(w, r); !ok {
		return
	}
	var req struct {
		OldID int64 `json:"oldId"`
		After int64 `json:"after"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.OldID < 1 || req.After < 0 {
		write400(w, "老人或游标无效")
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadGeofences)
	if !ok {
		return
	}
	defer tx.Rollback()
	items, err := fencerepo.ListEvents(r.Context(), tx, req.OldID, req.After)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"items": items})
}

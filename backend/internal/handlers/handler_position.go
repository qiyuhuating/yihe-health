package handlers

import (
	"net/http"
	"strconv"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/operationsrepo"
	"yihe-care/internal/response"
	"yihe-care/internal/securestore"
	"yihe-care/internal/telemetryrepo"
)

// Position coordinates and metadata are decrypted only after HTTP authorization.
func formatPositionRow(box *securestore.Box, raw telemetryrepo.PositionRow) (map[string]any, error) {
	lng, lat, err := openCoordinates(box, raw.Longitude, raw.Latitude)
	if err != nil {
		return nil, err
	}
	alarmLng, alarmLat, err := openCoordinates(box, raw.AlarmLongitude, raw.AlarmLatitude)
	if err != nil {
		return nil, err
	}
	position, err := openPositionMetadata(box, raw.PositionMetadata)
	if err != nil {
		return nil, err
	}
	alarm, err := openPositionMetadata(box, raw.AlarmMetadata)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name": raw.Name, "oldId": raw.ResidentID, "sex": raw.Sex,
		"lng": lng, "lat": lat,
		"beTime": tsOrNil(raw.ObservedAt), "status": raw.Status,
		"alarmTime": tsOrNil(raw.AlarmTime), "alarmLng": alarmLng, "alarmLat": alarmLat,
		"alarmKind": raw.AlarmKind, "alarmSource": alarm.Source,
		"alarmSampledAt": metadataSampleTime(alarm.SampledAt), "alarmAccuracy": alarm.Accuracy, "alarmValidity": alarm.Validity,
		"positionSource": position.Source, "positionSampledAt": metadataSampleTime(position.SampledAt),
		"positionAccuracy": position.Accuracy, "positionValidity": position.Validity,
	}, nil
}

// POST /api/v1/position/select: paginated latest positions for administrators.
func (h *Handler) positionSelect(w http.ResponseWriter, r *http.Request) {
	name, page, pageSize, ok := namedPage(w, r)
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadLocations)
	if !ok {
		return
	}
	defer tx.Rollback()
	rows, total, err := telemetryrepo.ListLatestPositions(r.Context(), tx, name, page, pageSize)
	if internalError(w, err) {
		return
	}
	positions := make([]map[string]any, 0, len(rows))
	for _, raw := range rows {
		view, err := formatPositionRow(h.Box, raw)
		if internalError(w, err) {
			return
		}
		positions = append(positions, view)
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{
		"positionDtos": positions, "total": total, "page": page, "pageSize": pageSize,
	})
}

func (h *Handler) positionAlertSummary(w http.ResponseWriter, r *http.Request) {
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadAlerts)
	if !ok {
		return
	}
	defer tx.Rollback()
	total, active, err := operationsrepo.ReadPositionAlerts(r.Context(), tx)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"totalResidents": total, "activeAlerts": active})
}

// POST /api/v1/position/selectFamily: latest location for an authorized resident.
func (h *Handler) positionSelectFamily(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	principal := auth.Principal(r)
	admin := principal != nil && principal.Role == auth.RoleAdmin
	rawID, _ := vstr(m, "oldId")
	var residentID int64
	if rawID == "" && !admin && principal != nil && principal.OldID != 0 {
		residentID = principal.OldID
	} else {
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			write400(w, "老人编号不正确")
			return
		}
		residentID = id
	}
	tx, ok := h.beginScopedLocationRead(w, r, residentID)
	if !ok {
		return
	}
	defer tx.Rollback()
	rows, err := telemetryrepo.ListResidentPositions(ctx, tx, residentID)
	if internalError(w, err) {
		return
	}
	positions := make([]map[string]any, 0, len(rows))
	for _, raw := range rows {
		view, err := formatPositionRow(h.Box, raw)
		if internalError(w, err) {
			return
		}
		positions = append(positions, view)
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"positionDtos": positions})
}

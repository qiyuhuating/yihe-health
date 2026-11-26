package handlers

import "net/http"

// routeResidentDevices registers resident records, devices, and location operations.
func (h *Handler) routeResidentDevices(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/residents/list", h.listResidents)
	mux.HandleFunc("POST /api/v1/residents/create", h.createResident)
	mux.HandleFunc("POST /api/v1/residents/update", h.updateResident)
	mux.HandleFunc("POST /api/v1/residents/delete", h.deleteResident)
	mux.HandleFunc("POST /api/v1/residents/details", h.getResidentDetails)

	mux.HandleFunc("POST /api/v1/device/login", h.deviceLogin)
	mux.HandleFunc("POST /api/v1/device/bind", h.deviceBind)
	mux.HandleFunc("POST /api/v1/device/heartbeat", h.deviceHeartbeat)
	mux.HandleFunc("POST /api/v1/device/report", h.deviceReport)
	mux.HandleFunc("POST /api/v1/device/sos", h.deviceSos)
	mux.HandleFunc("POST /api/v1/device/list", h.deviceList)
	mux.HandleFunc("POST /api/v1/device/createBindCode", h.deviceCreateBindCode)
	mux.HandleFunc("POST /api/v1/device/bindCodeList", h.deviceBindCodeList)
	mux.HandleFunc("POST /api/v1/device/deleteBindCode", h.deviceDeleteBindCode)
	mux.HandleFunc("POST /api/v1/device/create", h.deviceCreate)
	mux.HandleFunc("POST /api/v1/device/resetSecret", h.deviceResetSecret)
	mux.HandleFunc("POST /api/v1/device/unbind", h.deviceUnbind)
	mux.HandleFunc("POST /api/v1/device/delete", h.deviceDelete)
	mux.HandleFunc("POST /api/v1/locations/alerts/resolve-latest", h.resolveLatestLocationAlert)
	mux.HandleFunc("POST /api/v1/device/alertCounts", h.deviceAlertCounts)
	mux.HandleFunc("POST /api/v1/device/alertDetail", h.deviceAlertDetail)
	mux.HandleFunc("POST /api/v1/device/resolveAlert", h.deviceResolveAlert)

	mux.HandleFunc("POST /api/v1/monitoring-profiles/list", h.listMonitoringProfiles)
	mux.HandleFunc("POST /api/v1/monitoring-profiles/create", h.createMonitoringProfile)
	mux.HandleFunc("POST /api/v1/monitoring-profiles/update", h.updateMonitoringProfile)
	mux.HandleFunc("POST /api/v1/monitoring-profiles/delete", h.deleteMonitoringProfile)

	mux.HandleFunc("POST /api/v1/locations/alerts/summary", h.positionAlertSummary)
	mux.HandleFunc("POST /api/v1/position/select", h.positionSelect)
	mux.HandleFunc("POST /api/v1/position/selectFamily", h.positionSelectFamily)
}

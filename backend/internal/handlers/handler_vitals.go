package handlers

import "net/http"

func (h *Handler) routeVitals(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/vitals/alerts/count", h.countVitalAlerts)
	mux.HandleFunc("POST /api/v1/vitals/alerts/detail", h.getVitalAlert)
	mux.HandleFunc("POST /api/v1/vitals/alerts/resolve", h.handleResolveVitalAlert)
	mux.HandleFunc("POST /api/v1/vitals/latest", h.listLatestVitals)
	mux.HandleFunc("POST /api/v1/vitals/history", h.listVitalHistory)
	mux.HandleFunc("POST /api/v1/vitals/chart", h.getVitalChart)
	mux.HandleFunc("POST /api/v1/vitals/alerts/device-counts", h.countVitalAlertDevices)
	mux.HandleFunc("POST /api/v1/activity/latest", h.listLatestActivity)
	mux.HandleFunc("POST /api/v1/activity/history", h.listActivityHistory)
	mux.HandleFunc("POST /api/v1/activity/chart", h.getActivityChart)
	mux.HandleFunc("POST /api/v1/activity/alerts/device-counts", h.countSleepAlertDevices)
	mux.HandleFunc("POST /api/v1/health/alerts/device-counts", h.countHealthAlertDevices)
}

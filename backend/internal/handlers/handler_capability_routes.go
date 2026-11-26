package handlers

import (
	"net/http"
)

func (h *Handler) routeCapabilities(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/device-command-profiles", h.commandProfile)
	mux.HandleFunc("POST /api/v1/device-commands", h.commandCreate)
	mux.HandleFunc("POST /api/v1/device-commands/search", h.commandList)
	mux.HandleFunc("POST /api/v1/device/commandPull", h.commandPull)
	mux.HandleFunc("POST /api/v1/device/commandAck", h.commandAck)
	mux.HandleFunc("POST /api/v1/health/daily-query", h.healthDaily)
	mux.HandleFunc("POST /api/v1/fences", h.fenceSave)
	mux.HandleFunc("POST /api/v1/fences/search", h.fenceList)
	mux.HandleFunc("POST /api/v1/fence-events/search", h.fenceEvents)
	mux.HandleFunc("POST /api/v1/device/fencePoint", h.fencePoint)
	mux.HandleFunc("POST /api/v1/alert-reviews", h.alertReview)
	mux.HandleFunc("POST /api/v1/alert-reviews/search", h.alertReviews)
}

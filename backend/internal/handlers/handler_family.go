package handlers

import (
	"net/http"
)

// routeFamily registers family profiles and invitations.
func (h *Handler) routeFamily(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/family/select", h.familySelect)
	mux.HandleFunc("POST /api/v1/family/add", h.familyAdd)
	mux.HandleFunc("POST /api/v1/family/updateFamily", h.familyUpdate)
	mux.HandleFunc("POST /api/v1/family/deleteFamily", h.familyDelete)
	mux.HandleFunc("POST /api/v1/family/selectTwo", h.familySelectTwo)
	mux.HandleFunc("POST /api/v1/family/updateTwo", h.familyUpdateTwo)
	mux.HandleFunc("POST /api/v1/family/updateTouPic", h.familyUpdateTouPic)
	mux.HandleFunc("POST /api/v1/family/updatePassword", h.familyUpdatePassword)
	mux.HandleFunc("POST /api/v1/inviteCode/list", h.inviteList)
	mux.HandleFunc("POST /api/v1/inviteCode/create", h.inviteCreate)
	mux.HandleFunc("POST /api/v1/inviteCode/delete", h.inviteDelete)
	mux.HandleFunc("POST /api/v1/inviteCode/oldPersonList", h.inviteOldPersonList)
	mux.HandleFunc("POST /api/v1/inviteCode/checkCode", h.inviteCheckCode)
	mux.HandleFunc("POST /api/v1/inviteCode/join", h.inviteJoin)
}

package handlers

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"yihe-care/internal/careworkflow"
	"yihe-care/internal/response"
)

func (h *Handler) careCreateHandover(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	body, ok := bodyMap(w, r)
	if !ok {
		return
	}
	target, ok := requireID(w, body, "toAdminId")
	if !ok {
		return
	}
	note := strings.TrimSpace(str(body, "note"))
	if utf8.RuneCountInString(note) > 512 {
		write400(w, "交接备注不可超过512字符")
		return
	}

	handoverID, incidentIDs, err := careworkflow.CreateShiftHandover(r.Context(), h.DB, actor, target, note)
	switch {
	case errors.Is(err, careworkflow.ErrHandoverInvalid):
		writeValidation400(w)
		return
	case errors.Is(err, careworkflow.ErrHandoverDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "当前岗位不能发起该交接"})
		return
	case errors.Is(err, careworkflow.ErrHandoverEmpty):
		response.JSON(w, http.StatusConflict, map[string]any{"code": "HANDOVER_EMPTY", "message": "当前没有需要交接的在办任务"})
		return
	case errors.Is(err, careworkflow.ErrHandoverPending):
		response.JSON(w, http.StatusConflict, map[string]any{"code": "HANDOVER_PENDING", "message": "已有其他待确认交接单"})
		return
	case internalError(w, err):
		return
	}
	response.OK(w, map[string]any{"handoverId": handoverID, "incidentIds": incidentIDs})
}

func (h *Handler) careAcceptHandover(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	body, ok := bodyMap(w, r)
	if !ok {
		return
	}
	handoverID, ok := requireID(w, body, "handoverId")
	if !ok {
		return
	}

	transferred, err := careworkflow.AcceptShiftHandover(r.Context(), h.DB, handoverID, actor)
	switch {
	case errors.Is(err, careworkflow.ErrHandoverInvalid):
		writeValidation400(w)
		return
	case errors.Is(err, careworkflow.ErrHandoverDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "当前岗位不能接收该交接"})
		return
	case errors.Is(err, careworkflow.ErrHandoverMissing):
		response.JSON(w, http.StatusNotFound, map[string]any{"code": "HANDOVER_NOT_FOUND", "message": "交接单不存在"})
		return
	case errors.Is(err, careworkflow.ErrHandoverState):
		response.JSON(w, http.StatusConflict, map[string]any{"code": "HANDOVER_STATE", "message": "交接单已处理或不属于当前账号"})
		return
	case internalError(w, err):
		return
	}
	response.OK(w, map[string]any{"handoverId": handoverID, "transferred": transferred, "status": "accepted"})
}

func (h *Handler) careCancelHandover(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	body, ok := bodyMap(w, r)
	if !ok {
		return
	}
	handoverID, ok := requireID(w, body, "handoverId")
	if !ok {
		return
	}

	err := careworkflow.CancelShiftHandover(r.Context(), h.DB, handoverID, actor)
	switch {
	case errors.Is(err, careworkflow.ErrHandoverInvalid):
		writeValidation400(w)
		return
	case errors.Is(err, careworkflow.ErrHandoverDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "当前岗位不能取消该交接"})
		return
	case errors.Is(err, careworkflow.ErrHandoverMissing):
		response.JSON(w, http.StatusNotFound, map[string]any{"code": "HANDOVER_NOT_FOUND", "message": "交接单不存在"})
		return
	case errors.Is(err, careworkflow.ErrHandoverState):
		response.JSON(w, http.StatusConflict, map[string]any{"code": "HANDOVER_STATE", "message": "交接单已处理或不属于当前账号"})
		return
	case internalError(w, err):
		return
	}
	response.OK(w, map[string]any{"handoverId": handoverID, "status": "cancelled"})
}

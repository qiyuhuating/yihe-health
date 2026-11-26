package handlers

import (
	"errors"
	"net/http"
	"strings"

	"yihe-care/internal/careworkflow"
	"yihe-care/internal/commandworkflow"
	"yihe-care/internal/response"
)

func (h *Handler) commandCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		DeviceID   int64  `json:"deviceId"`
		Action     string `json:"action"`
		RequestKey string `json:"requestKey"`
		TTL        int    `json:"ttlSeconds"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.DeviceID < 1 || len(req.RequestKey) < 1 || len(req.RequestKey) > 64 || strings.TrimSpace(req.RequestKey) != req.RequestKey || req.TTL < 30 || req.TTL > 3600 {
		write400(w, "设备、幂等键或有效期无效（30–3600 秒）")
		return
	}
	result, err := commandworkflow.Create(r.Context(), h.DB, actor, commandworkflow.CreateRequest{
		DeviceID: req.DeviceID, Action: req.Action, RequestKey: req.RequestKey, TTL: req.TTL,
	})
	var invalid *commandworkflow.InvalidAction
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	case errors.Is(err, commandworkflow.ErrDeviceMissing):
		response.JSON(w, http.StatusNotFound, map[string]any{"message": "设备不存在"})
		return
	case errors.Is(err, commandworkflow.ErrDeviceUnavailable):
		conflict409(w, "设备须处于启用且已绑定状态")
		return
	case errors.Is(err, commandworkflow.ErrKeyConflict):
		conflict409(w, "幂等键已用于另一动作")
		return
	case errors.Is(err, commandworkflow.ErrProfileMissing):
		conflict409(w, "设备尚未启用颐和指令协议")
		return
	case errors.As(err, &invalid):
		write400(w, invalid.Error())
		return
	case internalError(w, err):
		return
	}
	if result.Duplicate {
		response.OK(w, map[string]any{"commandId": result.CommandID, "status": result.Status, "duplicate": true})
		return
	}
	response.OK(w, map[string]any{"commandId": result.CommandID, "status": result.Status})
}

func (h *Handler) commandList(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		DeviceID int64 `json:"deviceId"`
		After    int64 `json:"after"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.DeviceID < 1 || req.After < 0 {
		write400(w, "设备或游标无效")
		return
	}
	items, err := commandworkflow.List(r.Context(), h.DB, actor, req.DeviceID, req.After)
	if errors.Is(err, careworkflow.ErrCapabilityDenied) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	}
	if internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"items": items})
}

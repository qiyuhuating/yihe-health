package handlers

import (
	"errors"
	"net/http"

	"yihe-care/internal/careworkflow"
	"yihe-care/internal/commandrepo"
	"yihe-care/internal/commands"
	"yihe-care/internal/commandworkflow"
	"yihe-care/internal/response"
)

func (h *Handler) commandProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		DeviceID int64  `json:"deviceId"`
		WireType string `json:"wireType"`
		Protocol string `json:"protocol"`
		Enabled  bool   `json:"enabled"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if _, err := commands.Channel(req.WireType); err != nil || req.DeviceID < 1 || req.Protocol != "yihe-pull-v1" {
		write400(w, "须使用已知接口传值及 yihe-pull-v1 协议")
		return
	}
	id := req.DeviceID
	err := commandworkflow.Configure(r.Context(), h.DB, actor, id, commandrepo.Profile{
		WireType: req.WireType, Protocol: req.Protocol, Enabled: req.Enabled,
	})
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	case errors.Is(err, commandworkflow.ErrDeviceMissing):
		response.JSON(w, http.StatusNotFound, map[string]any{"message": "设备不存在"})
		return
	case internalError(w, err):
		return
	}
	response.OK(w, map[string]any{"deviceId": id, "enabled": req.Enabled})
}

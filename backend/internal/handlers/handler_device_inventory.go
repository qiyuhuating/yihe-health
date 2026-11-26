package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/devicecredential"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/response"
	"yihe-care/internal/securecode"
)

func (h *Handler) genDeviceNo(ctx context.Context) (string, error) {
	for i := 0; i < 30; i++ {
		s, err := securecode.Generate(8)
		if err != nil {
			return "", err
		}
		no := "W" + s
		exists, err := devicerepo.NumberExists(ctx, h.DB, no)
		if err != nil {
			return "", err
		}
		if !exists {
			return no, nil
		}
	}
	return "W" + time.Now().Format("20060102150405"), nil
}

// POST /api/v1/device/create —— 后台登记新手表（ADMIN，事务）。
func (h *Handler) deviceCreate(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	alias, model, oldID, ok := parseDeviceRegistration(m)
	if !ok {
		writeValidation400(w)
		return
	}
	ctx := r.Context()
	deviceNo, err := h.genDeviceNo(ctx)
	if internalError(w, err) {
		return
	}
	clearSecret, hashed, err := devicecredential.Generate()
	if internalError(w, err) {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageDevices)
	if !ok {
		return
	}
	defer tx.Rollback()
	now := time.Now()
	deviceID, err := devicerepo.Create(ctx, tx, devicerepo.NewDevice{
		Number: deviceNo, SecretHash: hashed, Alias: alias, Model: model,
		ResidentID: oldID, At: now, ProfileDay: siteDay(now),
	})
	switch {
	case errors.Is(err, devicerepo.ErrResidentNotFound):
		response.Fail(w, "老人不存在")
	case errors.Is(err, devicerepo.ErrDeviceInsertFailed):
		response.Fail(w, "新建设备失败")
	case internalError(w, err):
		return
	default:
		if internalError(w, tx.Commit()) {
			return
		}
		response.OK(w, map[string]any{
			"deviceId": deviceID, "deviceNo": deviceNo, "secret": clearSecret,
		})
	}
}

// POST /api/v1/device/resetSecret —— 重置设备密钥（ADMIN）。
func (h *Handler) deviceResetSecret(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	deviceID, ok := requireID(w, m, "deviceId")
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageDevices)
	if !ok {
		return
	}
	defer tx.Rollback()
	exists, err := devicerepo.Exists(r.Context(), tx, deviceID)
	if internalError(w, err) {
		return
	}
	if !exists {
		response.Fail(w, "设备不存在")
		return
	}
	clearSecret, hashed, err := devicecredential.Generate()
	if internalError(w, err) {
		return
	}
	changed, err := devicerepo.UpdateSecret(r.Context(), tx, deviceID, hashed)
	if internalError(w, err) {
		return
	}
	if !changed {
		response.Fail(w, "密钥更新失败，请重试")
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"secret": clearSecret})
}

// POST /api/v1/device/unbind —— 解除设备绑定（ADMIN，事务）。
func (h *Handler) deviceUnbind(w http.ResponseWriter, r *http.Request) {
	h.detachDevice(w, r, false)
}

// POST /api/v1/device/delete —— 删除设备（ADMIN，事务）。
func (h *Handler) deviceDelete(w http.ResponseWriter, r *http.Request) {
	h.detachDevice(w, r, true)
}

func (h *Handler) detachDevice(w http.ResponseWriter, r *http.Request, remove bool) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	deviceID, ok := requireID(w, m, "deviceId")
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageDevices)
	if !ok {
		return
	}
	defer tx.Rollback()
	changed, err := devicerepo.Detach(r.Context(), tx, deviceID, remove)
	if internalError(w, err) {
		return
	}
	if !changed {
		if remove {
			response.Fail(w, "删除失败")
		} else {
			response.Fail(w, "解绑失败")
		}
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, nil)
}

func parseDeviceRegistration(m map[string]any) (string, string, *int64, bool) {
	alias, _ := vstr(m, "alias")
	if alias == "" {
		alias = "新手表"
	}
	model, _ := vstr(m, "model")
	if model == "" {
		model = "HarmonyOS-Wear"
	}
	if utf8.RuneCountInString(alias) > 32 || utf8.RuneCountInString(model) > 32 {
		return "", "", nil, false
	}
	oldIDStr, _ := vstr(m, "oldId")
	var oldID *int64
	if oldIDStr != "" {
		if !validID(oldIDStr) {
			return "", "", nil, false
		}
		n, _ := strconv.ParseInt(oldIDStr, 10, 64)
		oldID = &n
	}
	return alias, model, oldID, true
}

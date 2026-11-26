package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
	"yihe-care/internal/config"

	"yihe-care/internal/devicecredential"
	"yihe-care/internal/deviceworkflow"
	"yihe-care/internal/health"
	"yihe-care/internal/response"
)

func (h *Handler) routeHealth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/device/healthBatch", h.healthBatch)
	mux.HandleFunc("POST /api/v1/health/changes", h.healthChanges)
	mux.HandleFunc("POST /api/v1/health/statistics", h.healthStatistics)
	mux.HandleFunc("POST /api/v1/health/latest", h.healthLatest)
	mux.HandleFunc("POST /api/v1/health/baseline", h.healthBaseline)
}

func checkHealthDeviceCredential(w http.ResponseWriter, supplied, stored string, status *int) bool {
	switch devicecredential.Check(supplied, stored, status) {
	case devicecredential.InvalidSecret:
		hwFail(w, "DEVICE_AUTH_FAILED", "设备号或密钥不正确")
		return false
	case devicecredential.Disabled:
		hwFail(w, "DEVICE_DISABLED", "设备已停用")
		return false
	}
	return true
}

func (h *Handler) healthBatch(w http.ResponseWriter, r *http.Request) {
	// Owner-first is opt-in only. All old-version writers must be stopped and
	// drained before enabling it: mixed device->owner and owner->device lock
	// ordering across replicas can produce InnoDB deadlocks.
	lateLock := h.Cfg.Requests.HealthLateLock
	if !h.Cfg.Requests.Loaded {
		lateLock = config.Enabled("HEALTH_LATE_DEVICE_LOCK")
	}
	h.healthBatchWithLockOrder(w, r, lateLock)
}

// healthBatchWithLockOrder preserves the original write/revision contract but
// can delay the device-row lock until the final binding/secret revalidation.
// Never enable owner-first concurrently with an old device-first writer.
func (h *Handler) healthBatchWithLockOrder(w http.ResponseWriter, r *http.Request, lateDeviceLock bool) {
	r, _, signedOK := h.prepareSignedDeviceRequest(w, r)
	if !signedOK {
		return
	}
	var req healthBatchRequest
	if !decodeHealthBatch(w, r, &req) {
		return
	}
	batch, ok := prepareHealthUpload(w, &req)
	if !ok {
		return
	}
	ctx := r.Context()
	if !h.healthWrites.acquire(ctx, req.DeviceNo) {
		healthWriteBusy(w)
		return
	}
	admitted := true
	defer func() {
		if admitted {
			h.healthWrites.release(req.DeviceNo)
		}
	}()
	// This read is advisory: the locked device row still authorizes every
	// write. Resolve the owner before BEGIN so conflicting devices can queue
	// without holding transaction connections.
	d, code, msg, err := h.healthDeviceAuth(ctx, h.DB, req.DeviceNo, req.Secret, "设备已停用")
	if internalError(w, err) {
		return
	}
	if d == nil {
		hwFail(w, code, msg)
		return
	}
	if d.No != req.DeviceNo {
		hwFail(w, "DEVICE_AUTH_FAILED", "设备号或密钥不正确，或设备已停用")
		return
	}
	if req.BindingVersion < d.HealthEpoch {
		response.Error(w, http.StatusConflict, string(deviceworkflow.EpochExpired), "绑定纪元已过期，请重新登录并清理旧纪元采样")
		return
	}
	if d.OldID == nil || d.HealthEpoch != req.BindingVersion {
		response.Error(w, http.StatusConflict, string(deviceworkflow.BindingChanged), "绑定已变化，请重新登录；旧绑定数据不能归入新老人")
		return
	}
	expectedOwner := *d.OldID
	admitted = h.healthWrites.claimOwner(ctx, req.DeviceNo, expectedOwner)
	if !admitted {
		healthWriteBusy(w)
		return
	}
	priority := h.sosPriorityEnabled()
	var tx *sql.Tx
	var conn *sql.Conn
	if priority {
		tx, conn, err = h.healthWrites.beginPriorityTx(ctx, h.DB.DB, req.DeviceNo, lateDeviceLock)
		if errors.Is(err, errHealthWriteBusy) {
			healthWriteBusy(w)
			return
		}
	} else {
		tx, err = h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	if internalError(w, err) {
		return
	}
	if conn != nil {
		defer conn.Close()
	}
	defer tx.Rollback()
	// In experimental late-lock mode, perform the expensive sample write
	// under the owner-clock lock before locking the device row. Revalidate the
	// full device binding under FOR UPDATE before committing anything.
	if !lateDeviceLock {
		d, err = scanHealthDevice(tx.QueryRowContext(ctx, `SELECT `+healthDeviceCols+` FROM device WHERE device_id=? FOR UPDATE`, d.ID))
		if internalError(w, err) {
			return
		}

	}
	if d.No != req.DeviceNo {
		hwFail(w, "DEVICE_AUTH_FAILED", "设备号或密钥不正确，或设备已停用")
		return
	}
	if !checkHealthDeviceCredential(w, req.Secret, d.Secret, d.Status) {
		return
	}
	if req.BindingVersion < d.HealthEpoch {
		response.Error(w, http.StatusConflict, string(deviceworkflow.EpochExpired), "绑定纪元已过期，请重新登录并清理旧纪元采样")
		return
	}
	if d.OldID == nil || *d.OldID != expectedOwner || d.HealthEpoch != req.BindingVersion {
		response.Error(w, http.StatusConflict, string(deviceworkflow.BindingChanged), "绑定已变化，请重新登录；旧绑定数据不能归入新老人")
		return
	}
	// The nonce and accepted samples share the same commit. In owner-first
	// mode defer nonce insertion until the final locked device validation.
	if !lateDeviceLock && !acceptSignedDeviceNonce(w, ctx, tx, d.ID) {
		return
	}
	owner := health.Owner{OldID: expectedOwner, DeviceID: &d.ID, Epoch: d.HealthEpoch}
	cursor, duplicates, err := batch.SaveProtected(ctx, tx, owner, h.Box)
	if err != nil {
		if errors.Is(err, health.ErrConflict) {
			conflict409(w, err.Error())
		} else {
			response.Error500(w, logInternalError(err))
		}
		return
	}
	if lateDeviceLock {
		// An SOS may finish while this upload holds only the owner clock.
		// Never wait for a pending SOS under that clock: a daily reader could
		// hold the resident lock while waiting for us, forming a wait cycle.
		// Roll back and let the device retry the original batch.
		if priority && !h.healthWrites.beginTransaction(req.DeviceNo, true) {
			healthWriteBusy(w)
			return
		}
		locked, lockErr := scanHealthDevice(tx.QueryRowContext(ctx, `SELECT `+healthDeviceCols+` FROM device WHERE device_id=? FOR UPDATE`, d.ID))
		if internalError(w, lockErr) {
			return
		}
		d = locked
		if d.No != req.DeviceNo {
			hwFail(w, "DEVICE_AUTH_FAILED", "设备号或密钥不正确")
			return
		}
		if !checkHealthDeviceCredential(w, req.Secret, d.Secret, d.Status) {
			return
		}
		if req.BindingVersion < d.HealthEpoch {
			response.Error(w, http.StatusConflict, string(deviceworkflow.EpochExpired), "绑定纪元已过期，请重新登录并清理旧纪元采样")
			return
		}
		if d.OldID == nil || *d.OldID != expectedOwner || d.HealthEpoch != req.BindingVersion {
			response.Error(w, http.StatusConflict, string(deviceworkflow.BindingChanged), "绑定已变化，请重新登录；旧绑定数据不能归入新老人")
			return
		}
		if !acceptSignedDeviceNonce(w, ctx, tx, d.ID) {
			return
		}
	}
	if err = touchHealthDevice(ctx, tx, d.ID, time.Now()); internalError(w, err) {
		return
	}
	if err = tx.Commit(); internalError(w, err) {
		return
	}
	if priority {
		// Return the explicit connection before advancing the derived projection.
		conn.Close()
	}
	// The committed transaction no longer owns a write conflict domain.
	h.healthWrites.release(req.DeviceNo)
	admitted = false
	h.advanceCoreAfterCommit(owner, &batch, req.Samples)
	// This is a write acknowledgement, not the client's read cursor.
	response.OK(w, map[string]any{"writeRevision": cursor, "accepted": len(req.Samples), "duplicates": duplicates})
}

func (h *Handler) healthChanges(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldID int64 `json:"oldId"`
		After int64 `json:"after"`
		Limit int   `json:"limit"`
	}
	if !decodeHealth(w, r, &req) {
		return
	}
	if req.After < 0 || req.After > health.MaxRevision || req.Limit < 0 || req.Limit > 200 {
		write400(w, "游标或分页参数无效")
		return
	}
	if req.Limit == 0 {
		req.Limit = 100
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	oldID := req.OldID
	if oldID == 0 {
		oldID = p.OldID
	}
	// The scoped transaction below checks current family ownership.
	tx, ok := h.beginScopedHealthRead(w, r, oldID)
	if !ok {
		return
	}
	defer tx.Rollback()
	changes, err := health.ListChanges(r.Context(), tx, oldID, req.After, req.Limit, h.Box)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{
		"changes": changes.Items, "nextCursor": changes.NextCursor,
		"hasMore": changes.HasMore, "oldId": oldID,
	})
}

type healthBatchRequest struct {
	DeviceNo       string          `json:"deviceNo"`
	Secret         string          `json:"secret"`
	BindingVersion int64           `json:"bindingVersion"`
	Samples        []health.Sample `json:"samples"`
}

func prepareHealthUpload(w http.ResponseWriter, req *healthBatchRequest) (health.PreparedBatch, bool) {
	if len(req.Samples) < 1 || len(req.Samples) > 200 {
		write400(w, "每批须为 1–200 条采样")
		return health.PreparedBatch{}, false
	}
	batch, err := health.PrepareDeviceBatch(req.DeviceNo, req.Samples)
	if err != nil {
		if errors.Is(err, health.ErrSourceDeviceMismatch) {
			write400(w, "采样来源设备与认证设备不一致")
		} else {
			write400(w, err.Error())
		}
		return health.PreparedBatch{}, false
	}
	return batch, true
}

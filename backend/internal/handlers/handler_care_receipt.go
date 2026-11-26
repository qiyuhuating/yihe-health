package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"yihe-care/internal/config"
	"time"

	"yihe-care/internal/response"
)

// This endpoint only accepts cryptographically authenticated delivery receipts
// from the configured gateway; HTTP dispatch success alone is not delivery.
func (h *Handler) careReceipt(w http.ResponseWriter, r *http.Request) {
	item, ok := h.decodeCareReceipt(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if internalError(w, err) {
		return
	}
	defer tx.Rollback()
	var incidentID int64
	var status string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT incident_id,status,attempts FROM care_notification_outbox
   WHERE id=? FOR UPDATE`, item.NotificationID).Scan(&incidentID, &status, &attempts)
	if err != nil {
		if isNotFound(err) {
			response.JSON(w, http.StatusConflict, map[string]any{"message": "回执状态不符合预期"})
		} else {
			response.Error500(w, ref())
		}
		return
	}
	if status == "delivered" {
		// A gateway can retry after our commit but before receiving HTTP 200.
		// Authenticate first, then ACK the durable duplicate without a second event.
		if internalError(w, tx.Commit()) {
			return
		}
		response.OK(w, map[string]any{"notificationId": item.NotificationID, "status": "delivered"})
		return
	}
	// A gateway may deliver after our request timed out and the outbox
	// became "failed". A signed receipt is authoritative once an attempt was
	// actually claimed; never accept a still-pending (unattempted) task.
	if status != "sending" && status != "sent" && !(status == "failed" && attempts > 0) {
		response.JSON(w, http.StatusConflict, map[string]any{"message": "回执状态不符合预期"})
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE care_notification_outbox SET status='delivered',delivered_at=CURRENT_TIMESTAMP(6) WHERE id=?`, item.NotificationID); internalError(w, err) {
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE care_incident SET state='notified',updated_at=CURRENT_TIMESTAMP(6)
    WHERE id=? AND state IN ('received','notifying')`, incidentID); err != nil {
		response.Error500(w, ref())
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO care_incident_event(incident_id,actor,action,note)
    VALUES(?,'gateway','delivered','通知渠道回执已通过签名验证')`, incidentID); err != nil {
		response.Error500(w, ref())
		return
	}
	if err = tx.Commit(); internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"notificationId": item.NotificationID, "status": "delivered"})
}

type careReceiptRequest struct {
	NotificationID int64  `json:"notificationId"`
	Status         string `json:"status"`
}

func (h *Handler) decodeCareReceipt(w http.ResponseWriter, r *http.Request) (careReceiptRequest, bool) {
	secret := h.Cfg.Requests.CareReceiptSecret
	if !h.Cfg.Requests.Loaded {
		secret = config.Secret("CARE_RECEIPT_SECRET")
	}
	if len(secret) < 32 {
		response.JSON(w, http.StatusServiceUnavailable, map[string]any{"message": "回执校验服务尚未配置"})
		return careReceiptRequest{}, false
	}
	stamp := r.Header.Get("X-Care-Timestamp")
	ts, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)) > 5*time.Minute ||
		time.Until(time.Unix(ts, 0)) > 5*time.Minute {
		response.JSON(w, http.StatusUnauthorized, map[string]any{"message": "回执时间无效"})
		return careReceiptRequest{}, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8193))
	if err != nil || len(body) > 8192 {
		response.JSON(w, http.StatusBadRequest, map[string]any{"message": "回执参数错误"})
		return careReceiptRequest{}, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	signature, err := hex.DecodeString(strings.TrimSpace(r.Header.Get("X-Care-Signature")))
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		response.JSON(w, http.StatusUnauthorized, map[string]any{"message": "回执签名无效"})
		return careReceiptRequest{}, false
	}
	var item careReceiptRequest
	if json.Unmarshal(body, &item) != nil || item.NotificationID < 1 || item.Status != "delivered" {
		response.JSON(w, http.StatusBadRequest, map[string]any{"message": "回执状态无效"})
		return careReceiptRequest{}, false
	}
	return item, true
}

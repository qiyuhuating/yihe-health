package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"yihe-care/internal/accesspolicy"
	"yihe-care/internal/response"
)

// EnforceDeviceSignatures extends mandatory device HMAC and nonce replay
// checks to hardware routes other than report/SOS. Those two endpoints already
// reserve their nonces inside the business transaction and must not do it
// twice. /api/v1/care/receipt is tagged Device in the access registry but uses
// a separate gateway HMAC, so it is excluded as well.
//
// The gate is opt-in via YIHE_REQUIRE_SIGNED_DEVICE=1 so existing unsigned
// firmware keeps working. Any signature that is present must still verify.
func (h *Handler) EnforceDeviceSignatures(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accesspolicy.AccessFor(r.Method, r.URL.Path) != accesspolicy.Device || deviceSignatureExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		signed, ok := h.prepareSignedDeviceRequest(w, r)
		if !ok {
			return
		}
		if _, present := signed.Context().Value(signedDeviceContextKey{}).(signedDeviceNonce); !present {
			next.ServeHTTP(w, signed)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(signed.Body, signedDeviceMaxBody+1))
		if err != nil || len(raw) > signedDeviceMaxBody {
			response.JSON(w, http.StatusBadRequest, map[string]any{"message": "设备请求体无效"})
			return
		}
		_ = signed.Body.Close()
		signed.Body = io.NopCloser(bytes.NewReader(raw))
		var credentials struct {
			DeviceNo string `json:"deviceNo"`
			Secret   string `json:"secret"`
		}
		if err := json.Unmarshal(raw, &credentials); err != nil {
			response.JSON(w, http.StatusBadRequest, map[string]any{"message": "设备请求体无效"})
			return
		}
		ctx := signed.Context()
		tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			response.Error500(w, ref())
			return
		}
		defer tx.Rollback()
		d, code, msg, err := h.hwAuth(ctx, tx, strings.ToUpper(strings.TrimSpace(credentials.DeviceNo)), credentials.Secret, "设备已停用")
		if err != nil {
			response.Error500(w, ref())
			return
		}
		if d == nil {
			hwFail(w, code, msg)
			return
		}
		if !acceptSignedDeviceNonce(w, ctx, tx, d.ID) {
			return
		}
		if err := tx.Commit(); err != nil {
			response.Error500(w, ref())
			return
		}
		next.ServeHTTP(w, signed)
	})
}

func deviceSignatureExempt(path string) bool {
	switch path {
	case "/api/v1/device/report", "/api/v1/device/sos", "/api/v1/care/receipt":
		return true
	default:
		return false
	}
}

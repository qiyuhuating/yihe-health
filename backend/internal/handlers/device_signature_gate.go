package handlers

import (
	"database/sql"
	"net/http"

	"yihe-care/internal/accesspolicy"
	"yihe-care/internal/response"
)

// EnforceDeviceSignatures extends mandatory device HMAC and nonce replay
// checks to hardware routes other than report/SOS/healthBatch. Those endpoints
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
		signed, credentials, ok := h.prepareSignedDeviceRequest(w, r)
		if !ok {
			return
		}
		if _, present := signed.Context().Value(signedDeviceContextKey{}).(signedDeviceNonce); !present {
			next.ServeHTTP(w, signed)
			return
		}
		ctx := signed.Context()
		tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			response.Error500(w, ref())
			return
		}
		defer tx.Rollback()
		d, code, msg, err := h.hwAuth(ctx, tx, credentials.DeviceNo, credentials.Secret, "设备已停用")
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
	case "/api/v1/device/report", "/api/v1/device/sos", "/api/v1/device/healthBatch", "/api/v1/care/receipt":
		return true
	default:
		return false
	}
}

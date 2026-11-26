package handlers

import (
	"net/http"
	"yihe-care/internal/response"
)

const tsLayout = "2006-01-02 15:04:05" // yyyy-MM-dd HH:mm:ss

// scopeDeny 归属隔离越权 → 403（与鉴权中间件同文案）。
func scopeDeny(w http.ResponseWriter) {
	response.JSON(w, http.StatusForbidden, map[string]any{
		"code": "ACCESS_DENIED", "message": "请求未获授权，请刷新页面后重试",
	})
}

// conflict409 并发冲突 → HTTP 409。
func conflict409(w http.ResponseWriter, message string) {
	response.JSON(w, http.StatusConflict, map[string]any{"message": message})
}

// writeSingleMutation replies with success only when one record changed.
func writeSingleMutation(w http.ResponseWriter, affected int64) {
	if affected == 1 {
		response.OK(w, nil)
		return
	}
	recordNotFound(w)
}

// Release transaction locks before returning an error to the client.
func rollbackFailure(w http.ResponseWriter, rollback func()) {
	rollback()
	response.Error500(w, ref())
}

func rollbackOnError(w http.ResponseWriter, rollback func(), err error) bool {
	if err == nil {
		return false
	}
	rollbackFailure(w, rollback)
	return true
}

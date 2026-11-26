// Package response defines the JSON contract shared by the web and watch clients.
package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

const (
	CodeOK   = "1111"
	CodeFail = "0000"
)

// JSON adds the result envelope without changing endpoint-specific data fields.
func JSON(w http.ResponseWriter, status int, fields map[string]any) {
	v := make(map[string]any, len(fields)+3)
	for key, value := range fields {
		v[key] = value
	}
	if status < 400 && v["returnCode"] != nil && v["returnCode"] != CodeOK {
		// Preserve legacy 400 semantics, but surface the caller's contradictory
		// success status so it can be corrected without breaking API clients.
		slog.Warn("api_response_success_status_with_failure_code", "http_status", status)
		status = http.StatusBadRequest
	}
	if status >= 400 {
		v["returnCode"] = CodeFail
		code, message := errorDefaults(status)
		if v["code"] == nil || v["code"] == "" {
			v["code"] = code
		}
		if v["message"] == nil || v["message"] == "" {
			v["message"] = message
		}
	} else {
		v["returnCode"] = CodeOK
	}
	writeJSON(w, status, v)
}

func OK(w http.ResponseWriter, fields map[string]any) {
	JSON(w, http.StatusOK, fields)
}

func Fail(w http.ResponseWriter, message string) {
	Error(w, http.StatusBadRequest, "INVALID_REQUEST", message)
}

// Error uses the already-defined typed envelope rather than allocating and
// copying an intermediate response map on every failed request.
func Error(w http.ResponseWriter, status int, code, message string) {
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	defaultCode, defaultMessage := errorDefaults(status)
	if code == "" {
		code = defaultCode
	}
	if message == "" {
		message = defaultMessage
	}
	writeJSON(w, status, BaseResponse{ReturnCode: CodeFail, Code: code, Message: message})
}

func Error500(w http.ResponseWriter, reference string) {
	writeJSON(w, http.StatusInternalServerError, BaseResponse{
		ReturnCode: CodeFail, Code: "INTERNAL_ERROR",
		Message: "服务处理失败", Reference: reference,
	})
}

func errorDefaults(status int) (string, string) {
	switch status {
	case 400:
		return "INVALID_REQUEST", "请求参数无效"
	case 401:
		return "AUTH_REQUIRED", "请登录后再操作"
	case 403:
		return "ACCESS_DENIED", "没有此操作权限"
	case 404:
		return "NOT_FOUND", "记录或接口不存在"
	case 405:
		return "METHOD_NOT_ALLOWED", "请求方法不受支持"
	case 409:
		return "CONFLICT", "数据状态已变化，请刷新后重试"
	case 413:
		return "PAYLOAD_TOO_LARGE", "请求数据超过允许大小"
	case 415:
		return "UNSUPPORTED_MEDIA_TYPE", "请求内容类型不受支持"
	case 429:
		return "RATE_LIMITED", "请求过于频繁，请稍后重试"
	case 500:
		return "INTERNAL_ERROR", "服务处理失败"
	case 503:
		return "SERVICE_UNAVAILABLE", "服务暂不可用"
	default:
		return "INTERNAL_ERROR", "服务处理失败"
	}
}

// writeJSON validates encoding before committing the HTTP status. No internal
// serialization error or partially encoded success body reaches a client.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		slog.Error("API response serialization failed", "error", err)
		status = http.StatusInternalServerError
		code, message := errorDefaults(status)
		encoded, _ = json.Marshal(BaseResponse{ReturnCode: CodeFail, Code: code, Message: message})
	}
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte{'\n'})
}

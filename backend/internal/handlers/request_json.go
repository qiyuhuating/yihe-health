package handlers

import (
	"encoding/json"
	"io"
	"net/http"
)

type jsonBodyPolicy struct {
	maxBytes      int64
	allowEmpty    bool
	rejectUnknown bool
	invalid       string
	trailing      string
}

// decodeJSONBody owns body limits, cleanup and the single-document check.
// Endpoint policies retain their existing size, field and error contracts.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, policy jsonBodyPolicy) bool {
	body := http.MaxBytesReader(w, r.Body, policy.maxBytes)
	defer body.Close()
	decoder := json.NewDecoder(body)
	if policy.rejectUnknown {
		decoder.DisallowUnknownFields()
	}
	err := decoder.Decode(dst)
	if policy.allowEmpty && err == io.EOF {
		return true
	}
	if err != nil {
		write400(w, policy.invalid)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		write400(w, policy.trailing)
		return false
	}
	return true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any, optional bool) bool {
	return decodeJSONBody(w, r, dst, jsonBodyPolicy{
		maxBytes: 1 << 20, allowEmpty: optional,
		invalid: "请求参数格式错误", trailing: "请求参数格式错误",
	})
}

func decodeHealth(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBody(w, r, dst, jsonBodyPolicy{
		maxBytes: 2 << 20,
		invalid:  "健康数据 JSON 无效或超出 2 MiB", trailing: "只允许一个 JSON 对象",
	})
}

func decodeCapability(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBody(w, r, dst, jsonBodyPolicy{
		maxBytes: 2 << 20, rejectUnknown: true,
		invalid: "参数含未知字段、JSON 无效或超出 2 MiB", trailing: "只允许一个 JSON 对象",
	})
}

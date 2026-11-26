package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"yihe-care/internal/response"
)

// optBody parses optional JSON; an empty body produces an empty map.
func optBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	if r.Body == nil {
		return map[string]any{}, true
	}
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		write400(w, "请求参数格式错误或超出 1 MiB")
		return nil, false
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, true
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]any{"message": "请求参数格式错误"})
		return nil, false
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, true
}

// strictPage requires integer page 1–100000 and pageSize 1–100.
func strictPage(w http.ResponseWriter, m map[string]any) (int, int, bool) {
	p, ok := strictPageVal(w, m, "page", 1)
	if !ok {
		return 0, 0, false
	}
	ps, ok := strictPageVal(w, m, "pageSize", 20)
	if !ok {
		return 0, 0, false
	}
	if p < 1 || p > 100000 || ps < 1 || ps > 100 {
		response.JSON(w, http.StatusBadRequest, map[string]any{"message": "分页参数超出允许范围"})
		return 0, 0, false
	}
	return p, ps, true
}

func strictPageVal(w http.ResponseWriter, m map[string]any, key string, fallback int) (int, bool) {
	if m[key] == nil {
		return fallback, true
	}
	n, ok := numParam(m, key)
	if !ok {
		write400(w, "分页参数必须为整数")
		return 0, false
	}
	if n < 1 || n > 100000 {
		write400(w, "分页参数超出允许范围")
		return 0, false
	}
	return int(n), true
}

// numParam accepts a required integer as either a JSON number or a string.
func numParam(m map[string]any, key string) (int64, bool) {
	return integerParam(m[key])
}

// strID 取数字 id 参数的字符串形态（字符串或整数 JSON 值）。
func strID(m map[string]any, key string) string {
	v, exists := m[key]
	if !exists || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	}
	return ""
}

// isDigits reports whether a nonempty string contains only digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// safeSub extracts a bounded byte range from an ASCII timestamp.
func safeSub(s string, begin, end int) string {
	if begin < 0 {
		begin = 0
	}
	if begin > len(s) {
		begin = len(s)
	}
	if end < begin {
		end = begin
	}
	if end > len(s) {
		end = len(s)
	}
	return s[begin:end]
}

func writeCode(w http.ResponseWriter, ok bool) {
	if !ok {
		response.Fail(w, "操作未完成")
		return
	}
	response.OK(w, nil)
}

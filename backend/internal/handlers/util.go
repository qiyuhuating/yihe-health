// 通用 handler 工具：JSON body 解析、分页参数等。
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"yihe-care/internal/response"
)

// bodyMap decodes a JSON object while allowing endpoint-specific fields.
func bodyMap(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	return decodeBodyMap(w, r, false)
}

func bodyMapOrEmpty(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	return decodeBodyMap(w, r, true)
}

func decodeBodyMap(w http.ResponseWriter, r *http.Request, optional bool) (map[string]any, bool) {
	var m map[string]any
	if !decodeBody(w, r, &m, optional) {
		return nil, false
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, true
}

// str 取 map 中的字符串字段。
func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// vstrMax 读取必填字符串并按字符数（rune，不是字节）校验上限。
//
// 没有这一层时，超长输入会一路走到 INSERT，由 MySQL 1406 拒绝，最终表现为
// 500「服务处理失败」——调用方既不知道是自己的输入有问题，也不知道该改哪里。
// limit<=0 表示不校验长度。
func vstrMax(w http.ResponseWriter, m map[string]any, key string, limit int) (string, bool) {
	value, present := vstr(m, key)
	if !present {
		response.Fail(w, "请求参数不完整")
		return "", false
	}
	if limit > 0 {
		if n := utf8.RuneCountInString(value); n > limit {
			response.Fail(w, fmt.Sprintf("字段 %s 最多 %d 个字符，当前 %d 个", key, limit, n))
			return "", false
		}
	}
	return value, true
}

// vstrRequired 必填语义：字段必须存在、非空白、且不超过 rune 上限。
func vstrRequired(w http.ResponseWriter, m map[string]any, key string, limit int) (string, bool) {
	value, ok := vstrMax(w, m, key, limit)
	if !ok {
		return "", false
	}
	if strings.TrimSpace(value) == "" {
		response.Fail(w, "参数不完整")
		return "", false
	}
	return value, true
}

// int64Val parses integer-valued fields without accepting overflow or fractional values.
func int64Val(m map[string]any, key string) (int64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n < math.MinInt64 || n >= 9223372036854775808 {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	}
	return 0, false
}

// bodyPage 从 body map 取分页（默认 1/20）。
//
// 越界不再静默夹取：客户端传了 pageSize=999 却拿到 20 条，会以为自己取到了
// 全量数据。参数不合法一律 400，让调用方明确知道请求被拒绝。
func bodyPage(w http.ResponseWriter, m map[string]any) (int, int, bool) {
	p, ps := 1, 20
	if v, present := m["page"]; present && v != nil {
		n, ok := int64Val(m, "page")
		if !ok || n < 1 || n > 100000 {
			write400(w, "分页参数超出允许范围")
			return 0, 0, false
		}
		p = int(n)
	}
	if v, present := m["pageSize"]; present && v != nil {
		n, ok := int64Val(m, "pageSize")
		if !ok || n < 1 || n > 100 {
			write400(w, "分页参数超出允许范围")
			return 0, 0, false
		}
		ps = int(n)
	}
	return p, ps, true
}

// ref generates a UUID for an internal error reference.
func ref() string { return uuid.NewString() }

// isNotFound recognizes wrapped missing-row errors.
func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// isUniqueViolation 判断唯一键冲突（MySQL 1062）。
//
// 「先查存在再插入」与插入不在同一把锁上，并发下必然撞唯一索引。没有这一层时
// 冲突会被当作内部错误返回 500，调用方看到的是「服务处理失败」，既无法重试也
// 不知道是数据重复。
func isUniqueViolation(err error) bool {
	var myErr *mysqlDriver.MySQLError
	return errors.As(err, &myErr) && myErr.Number == 1062
}

// duplicateConflict 把唯一键冲突写成 409；返回 false 表示不是冲突，调用方继续走
// 原有的错误分支。
func duplicateConflict(w http.ResponseWriter, err error, message string) bool {
	if !isUniqueViolation(err) {
		return false
	}
	response.Error(w, http.StatusConflict, "CONFLICT", message)
	return true
}

// internalError reports unexpected errors with an opaque reference.
//
// reference 会同时写进日志：过去 err 被直接丢弃、编号也不落任何地方，线上 500
// 只能看到一串 UUID，事后完全无法复盘。
func internalError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	reference := ref()
	var dbErr *mysqlDriver.MySQLError
	if errors.As(err, &dbErr) {
		log.Printf("internal error ref=%s mysql_code=%d", reference, dbErr.Number)
	} else {
		log.Printf("internal error ref=%s type=%T", reference, err)
	}
	if retryableError(err) {
		w.Header().Set("Retry-After", "2")
		response.Error(w, http.StatusServiceUnavailable, "SERVICE_BUSY", "服务暂时繁忙，请稍后重试")
		return true
	}
	response.Error500(w, reference)
	return true
}

// retryableError recognizes explicit database contention or resource exhaustion.
// An ambiguous network or commit failure must not imply a safe replay.
func retryableError(err error) bool {
	var dbErr *mysqlDriver.MySQLError
	return errors.As(err, &dbErr) && (dbErr.Number == 1213 || dbErr.Number == 1205 || dbErr.Number == 1040 || dbErr.Number == 1041)
}

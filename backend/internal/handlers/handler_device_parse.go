package handlers

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"yihe-care/internal/config"
	"yihe-care/internal/response"
	"time"
	"unicode/utf8"
)

func write400(w http.ResponseWriter, msg string) {
	response.JSON(w, http.StatusBadRequest, map[string]any{"message": msg})
}

func writeValidation400(w http.ResponseWriter) {
	write400(w, "请求字段不完整或超出允许范围")
}

// recordNotFound reports a missing record.
func recordNotFound(w http.ResponseWriter) {
	response.Error(w, http.StatusNotFound, "NOT_FOUND", "记录不存在")
}

// tsOrNil 时间戳 → "yyyy-MM-dd HH:mm:ss"，nil 保持 null。
func tsOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(tsLayout)
}

// dateOrNil 日期 → "yyyy-MM-dd"，nil 保持 null。
func dateOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

var (
	idPattern    = regexp.MustCompile(`^[0-9]{1,9}$`)
	reportTimeRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)
	secretHashRe = regexp.MustCompile(`^sha256\$[A-Za-z0-9_-]{43}$`)
)

// siteTimeZone 是机构所在地的时区，用于把「现场的今天」换算成 DATE 列的值。
//
// 数据库连接固定 time_zone='+00:00'、驱动 Loc=UTC，传进去的 time.Time 会先被
// 换算成 UTC 瞬间再落库。若用 time.Date(..., time.Local) 构造日期，结果会随
// 进程时区漂移：容器默认 UTC 时，北京时间 00:00–08:00 这段「今天」在 UTC 仍是
// 前一天，日期列会静默写成昨天；宿主为东八区时又会被换算成前天 16:00。
//
// 固定 +8 而不是 time.Local：中国全境无夏令时，且不依赖部署环境；也不引入
// time.LoadLocation，避免为了一个时区拖进 time/tzdata。
var siteTimeZone = func() *time.Location {
	if raw := strings.TrimSpace(config.Setting("SITE_TZ_OFFSET_HOURS")); raw != "" {
		if hours, err := strconv.ParseFloat(raw, 64); err == nil && hours >= -12 && hours <= 14 {
			return time.FixedZone("SITE", int(hours*3600))
		}
	}
	return time.FixedZone("CST", 8*3600)
}()

// siteDay 返回机构现场「今天」的日期值，专用于写 DATE 列。
func siteDay(now time.Time) time.Time {
	d := now.In(siteTimeZone)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

func validID(s string) bool { return idPattern.MatchString(s) }

func requireID(w http.ResponseWriter, body map[string]any, key string) (int64, bool) {
	value, _ := vstr(body, key)
	if !validID(value) {
		writeValidation400(w)
		return 0, false
	}
	id, _ := strconv.ParseInt(value, 10, 64)
	return id, true
}

// vstr 取字段的字符串形式（trim 后）；JSON 数字保留小数，整数字段由各自解析器校验。
// 返回 (值, 是否存在)。缺失/显式 null → ("", false)。
func vstr(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	switch n := v.(type) {
	case string:
		return strings.TrimSpace(n), true
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(n), true
	case json.Number:
		return strings.TrimSpace(n.String()), true
	default:
		return "NaN", true
	}
}

// 非整数 → 400"分页参数必须为整数"；超出 1–100000 / 1–100 → 400"分页参数超出允许范围"。
func pageFromBody(w http.ResponseWriter, m map[string]any) (int, int, bool) {
	p, msg := pageIntVal(m, "page", 1)
	if msg != "" {
		write400(w, msg)
		return 0, 0, false
	}
	ps, msg := pageIntVal(m, "pageSize", 20)
	if msg != "" {
		write400(w, msg)
		return 0, 0, false
	}
	if p < 1 || p > 100000 || ps < 1 || ps > 100 {
		write400(w, "分页参数超出允许范围")
		return 0, 0, false
	}
	return p, ps, true
}

func pageIntVal(m map[string]any, key string, fallback int) (int, string) {
	v, ok := m[key]
	if !ok || v == nil {
		return fallback, ""
	}
	switch value := v.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return fallback, ""
		}
	case float64:
	default:
		return 0, "分页参数必须为整数"
	}
	n, ok := integerParam(v)
	if !ok {
		return 0, "分页参数必须为整数"
	}
	if n < 1 || n > 100000 {
		return 0, "分页参数超出允许范围"
	}
	return int(n), ""
}

// 缺失/空 → nil；0 恒合法（设备协议里 0 表示无测量）；否则必须在 [min,max] 内，
// 非法 → "{key} 超出允许范围"。
func parseMetric(m map[string]any, key string, min, max int) (*int, string) {
	s, present := vstr(m, key)
	if !present || s == "" {
		return nil, ""
	}
	n, err := strconv.Atoi(s)
	if err != nil || (n != 0 && (n < min || n > max)) {
		return nil, key + " 超出允许范围"
	}
	return &n, ""
}

// 缺失/空 → nil；超长 → "{key} 超出允许长度"。
func parseText(m map[string]any, key string, maxLen int) (*string, string) {
	s, present := vstr(m, key)
	if !present || s == "" {
		return nil, ""
	}
	if utf8.RuneCountInString(s) > maxLen {
		return nil, key + " 超出允许长度"
	}
	return &s, ""
}

// 都空 → 合法；否则必须成对、≤20 字符、可解析、|lng|≤180、|lat|≤90。
func checkPosition(lng, lat string) string {
	if lng == "" && lat == "" {
		return ""
	}
	if utf8.RuneCountInString(lng) > 20 || utf8.RuneCountInString(lat) > 20 {
		return "经纬度应成对提供，且处于有效范围"
	}
	lo, err1 := strconv.ParseFloat(lng, 64)
	la, err2 := strconv.ParseFloat(lat, 64)
	if err1 != nil || err2 != nil || math.IsNaN(lo) || math.IsNaN(la) ||
		math.IsInf(lo, 0) || math.IsInf(la, 0) ||
		math.Abs(lo) > 180 || math.Abs(la) > 90 {
		return "经纬度应成对提供，且处于有效范围"
	}
	return ""
}

func parseReportTimeStrict(s string) (time.Time, bool) {
	if !reportTimeRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse(tsLayout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

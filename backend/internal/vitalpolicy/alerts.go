package vitalpolicy

import (
	"strconv"
	"strings"
)

// Alert rendering policy is pure: routes and persistence stay outside this package.
func Thresholds(first, last *int, low, high int) (int, int) {
	if first != nil && last != nil {
		return min(*first, *last), max(*first, *last)
	}
	return low, high
}

func Reason(metric string, value, low, high int) string {
	switch {
	case value > high:
		return metric + "偏高"
	case value < low:
		return metric + "偏低"
	default:
		return "历史" + metric + "异常"
	}
}

func DescribeRatepreAlert(a Alert) map[string]any {
	hr, bp := 0, 0
	if a.Hr != nil {
		hr = int(*a.Hr)
	}
	if a.Bp != nil {
		bp = int(*a.Bp)
	}
	rateBad := a.RateState != nil && *a.RateState == "1" && !a.RateDone
	preBad := a.PreState != nil && *a.PreState == "1" && !a.PreDone
	var reasons, details []string
	if rateBad {
		low, high := Thresholds(a.HrHigh, a.HrLow, 55, 110)
		reasons = append(reasons, Reason("心率", hr, low, high))
		details = append(details, "心率当前档案范围 "+strconv.Itoa(low)+"-"+strconv.Itoa(high)+" 次/分，本次测量 "+strconv.Itoa(hr)+" 次/分。")
	}
	if preBad {
		low, high := Thresholds(a.BpHigh, a.BpLow, 90, 140)
		reasons = append(reasons, Reason("血压", bp, low, high))
		details = append(details, "血压当前档案范围 "+strconv.Itoa(low)+"-"+strconv.Itoa(high)+" mmHg，本次测量 "+strconv.Itoa(bp)+" mmHg。")
	}
	if len(reasons) == 0 {
		return map[string]any{"hasAlert": 0, "message": "该记录已处理，没有待处理的红点"}
	}
	rb, pb := 0, 0
	if rateBad {
		rb = 1
	}
	if preBad {
		pb = 1
	}
	return map[string]any{
		"hasAlert":      1,
		"recordId":      a.ID,
		"deviceId":      a.DeviceID,
		"oldId":         a.OldID,
		"oldPersonName": a.OldPersonName,
		"hr":            hr,
		"bp":            bp,
		"rateBad":       rb,
		"preBad":        pb,
		"occurTime":     a.CreateTime,
		"reason":        strings.Join(reasons, "；"),
		"detail":        strings.Join(details, "；"),
		"advice":        "① 先联系家属确认老人身体状况；② 持续异常建议就医；③ 若是阈值设置不合理，到「基础管理-手表信息」调整心率/血压上下限；④ 确认无误后点「标记已处理」消除红点。",
	}
}

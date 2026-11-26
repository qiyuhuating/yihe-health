package handlers

import (
	"testing"
	"time"
)

// DATE 列（老人档案 be_time、阈值档案 op_time、默认档案 create_time）必须落在
// 机构所在地的「今天」。连接固定 time_zone='+00:00'、驱动 Loc=UTC，所以这里既
// 验证取的是现场日历，也验证写入后仍落在同一天。
func TestSiteDayUsesSiteCalendar(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time // 一律用 UTC 表示，模拟数据库连接所在的时区
		want string
	}{
		{
			// 北京 2026-11-03 01:30，UTC 仍是 11-02 —— 这是最容易写错成前一天的窗口。
			name: "before UTC midnight but already next day in CST",
			now:  time.Date(2026, 11, 2, 17, 30, 0, 0, time.UTC),
			want: "2026-11-03",
		},
		{
			name: "same day in both zones",
			now:  time.Date(2026, 11, 3, 6, 0, 0, 0, time.UTC),
			want: "2026-11-03",
		},
		{
			// 北京 2026-11-03 23:30，UTC 同日 15:30。
			name: "late evening in CST still same day",
			now:  time.Date(2026, 11, 3, 15, 30, 0, 0, time.UTC),
			want: "2026-11-03",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := siteDay(tc.now)
			if formatted := got.Format("2006-01-02"); formatted != tc.want {
				t.Fatalf("siteDay(%s) = %s，期望 %s", tc.now.Format(time.RFC3339), formatted, tc.want)
			}
			// 驱动写库前会执行 v.In(UTC)，DATE 列取的就是这个日期。
			if stored := got.In(time.UTC).Format("2006-01-02"); stored != tc.want {
				t.Fatalf("落库日期 = %s，期望 %s", stored, tc.want)
			}
			if got.Location() != time.UTC {
				t.Fatalf("siteDay 必须返回 UTC 零点，实际 location=%v", got.Location())
			}
		})
	}
}

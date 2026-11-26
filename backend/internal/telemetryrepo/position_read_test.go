package telemetryrepo

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func TestLatestPositionTimeFollowsCoordinates(t *testing.T) {
	if !strings.Contains(latestPositionsSQL, "l.lng, l.lat, l.be_time AS be_time") {
		t.Fatalf("position time must belong to the coordinates: %s", latestPositionsSQL)
	}
}

// Keep the previous window query here as an independently executable reference.
// The production query must preserve all rows, nulls, tie breaks and page totals.
const windowLatestPositionsSQL = "SELECT o.name, o.old_id, o.sex, l.lng, l.lat, l.be_time AS be_time, " +
	"COALESCE(a.status, '0') AS status, a.be_time AS alarm_time, a.lng AS alarm_lng, a.lat AS alarm_lat, " +
	"l.position_metadata AS location_metadata, a.position_metadata AS alarm_metadata, COALESCE(a.kind,'unknown') AS alarm_kind " +
	"FROM oldperson o " +
	"LEFT JOIN (SELECT old_id, lng, lat, be_time, position_metadata, " +
	"ROW_NUMBER() OVER (PARTITION BY old_id ORDER BY be_time DESC, id DESC) AS rn FROM device_location) l " +
	"ON l.old_id = o.old_id AND l.rn = 1 " +
	"LEFT JOIN (SELECT old_id, status, be_time, lng, lat, kind, position_metadata, " +
	"ROW_NUMBER() OVER (PARTITION BY old_id ORDER BY be_time DESC, id DESC) AS rn FROM alarm_precess) a " +
	"ON a.old_id = o.old_id AND a.rn = 1"

func windowLatestPositionPage(ctx context.Context, store db.ReadQuerier, name string, page, pageSize int) ([]PositionRow, int64, error) {
	const filter = " WHERE (?='' OR o.name LIKE CONCAT('%',?,'%'))"
	var total int64
	if err := store.QueryRowContext(ctx, "SELECT COUNT(*) FROM oldperson o"+filter, name, name).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := db.QueryList(ctx, store, scanPosition,
		windowLatestPositionsSQL+filter+" ORDER BY be_time DESC, o.old_id LIMIT ? OFFSET ?",
		name, name, pageSize, (page-1)*pageSize)
	return items, total, err
}

func TestIndexedLatestPositionsMatchWindowMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	statements := []string{
		"INSERT INTO oldperson(old_id,name,sex) VALUES (1,'Alice','女'),(2,'Bob','男'),(3,'Alice Jr',NULL),(4,NULL,NULL),(5,'Empty',NULL)",
		"INSERT INTO device_location(id,old_id,lng,lat,be_time) VALUES " +
			"(10,1,'older','20','2026-10-19 10:00:00')," +
			"(11,1,'same-time-old','21','2026-10-20 10:00:00')," +
			"(12,1,'same-time-new','22','2026-10-20 10:00:00')," +
			"(20,2,'no-time','20',NULL),(21,2,'with-time','21','2026-10-20 08:00:00')," +
			"(40,4,'null-time','40',NULL)",
		"INSERT INTO alarm_precess(id,old_id,status,be_time,kind) VALUES " +
			"(100,1,'1','2026-10-20 12:00:00','fence')," +
			"(101,1,'0','2026-10-20 12:00:00','fall')," +
			"(102,2,'1',NULL,'fence'),(103,3,'1','2026-10-20 13:00:00','fall')",
	}
	for _, statement := range statements {
		if _, err := pool.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name       string
		page, size int
	}{
		{"", 1, 2}, {"", 2, 2}, {"", 3, 2}, {"", 1, 20},
		{"Alice", 1, 10}, {"Alice", 2, 1}, {"absent", 1, 10},
	}
	for _, tc := range cases {
		got, total, err := ListLatestPositions(ctx, pool, tc.name, tc.page, tc.size)
		if err != nil {
			t.Fatal(err)
		}
		want, referenceTotal, err := windowLatestPositionPage(ctx, pool, tc.name, tc.page, tc.size)
		if err != nil {
			t.Fatal(err)
		}
		if total != referenceTotal || !reflect.DeepEqual(got, want) {
			t.Fatalf("name=%q page=%d size=%d: got total=%d rows=%+v, reference total=%d rows=%+v",
				tc.name, tc.page, tc.size, total, got, referenceTotal, want)
		}
	}
	rows, _, err := ListLatestPositions(ctx, pool, "Alice", 1, 10)
	if err != nil || len(rows) != 2 || rows[0].Longitude == nil ||
		*rows[0].Longitude != "same-time-new" || rows[0].AlarmKind != "fall" {
		t.Fatalf("same-timestamp tie break changed: %+v, %v", rows, err)
	}
}

// Run with TEST_MYSQL_DSN set. The two modes use identical schema, rows,
// filtering, pagination and scanning. Do not treat one synthetic run as a
// production latency claim; record the source SHA and MySQL version.
func BenchmarkLatestPositionsQuery(b *testing.B) {
	pool := dbtest.MySQL(b)
	ctx := context.Background()
	const residents, history = 100, 150
	for id := 1; id <= residents; id++ {
		if _, err := pool.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES (?,?)",
			id, fmt.Sprintf("R%03d", id)); err != nil {
			b.Fatal(err)
		}
	}
	for _, table := range []string{"device_location", "alarm_precess"} {
		batch := make([]string, 0, 200)
		flush := func() {
			if len(batch) == 0 {
				return
			}
			var sql string
			if table == "device_location" {
				sql = "INSERT INTO device_location(old_id,lng,lat,be_time) VALUES "
			} else {
				sql = "INSERT INTO alarm_precess(old_id,status,be_time) VALUES "
			}
			if _, err := pool.ExecContext(ctx, sql+strings.Join(batch, ",")); err != nil {
				b.Fatal(err)
			}
			batch = batch[:0]
		}
		for id := 1; id <= residents; id++ {
			for sample := 0; sample < history; sample++ {
				if table == "alarm_precess" && sample%4 != 0 {
					continue
				}
				ts := fmt.Sprintf("2026-10-%02d 12:%02d:00", sample%28+1, sample%60)
				if table == "device_location" {
					batch = append(batch, fmt.Sprintf("(%d,'113','23','%s')", id, ts))
				} else {
					batch = append(batch, fmt.Sprintf("(%d,'1','%s')", id, ts))
				}
				if len(batch) == cap(batch) {
					flush()
				}
			}
		}
		flush()
	}
	reference, referenceTotal, err := windowLatestPositionPage(ctx, pool, "", 1, 25)
	if err != nil {
		b.Fatal(err)
	}
	indexed, indexedTotal, err := ListLatestPositions(ctx, pool, "", 1, 25)
	if err != nil || indexedTotal != referenceTotal || !reflect.DeepEqual(indexed, reference) {
		b.Fatalf("queries disagree before benchmark: %v", err)
	}
	for _, mode := range []struct {
		name  string
		query func(context.Context, db.ReadQuerier, string, int, int) ([]PositionRow, int64, error)
	}{
		{"window", windowLatestPositionPage},
		{"indexed", ListLatestPositions},
	} {
		b.Run(mode.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, total, err := mode.query(ctx, pool, "", 1, 25)
				if err != nil || total != residents {
					b.Fatalf("query error=%v, total=%d", err, total)
				}
			}
		})
	}
}

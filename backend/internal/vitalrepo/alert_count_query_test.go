package vitalrepo

import (
	"context"
	"database/sql"
	"testing"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func TestPendingCountsMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	rate, pressure, err := AlertCounts(ctx, tx)
	if err != nil || rate != 0 || pressure != 0 {
		t.Fatalf("empty history: rate=%d pressure=%d err=%v", rate, pressure, err)
	}
	for _, query := range []string{
		`INSERT INTO device(device_id,device_no,secret,status) VALUES (1,'A','a',1),(2,'B','b',1),(3,'C','c',0)`,
		`INSERT INTO ratepre(device_id,rate_state,pre_state,create_time,rate_handled_at) VALUES
			(1,'1','1','2026-01-02',NULL),(1,'1',NULL,'2026-01-02','2026-01-02'),
			(1,'1','0','2026-01-02','2026-01-01'),(3,NULL,'1','2026-01-02',NULL)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	rate, pressure, err = AlertCounts(ctx, tx)
	if err != nil || rate != 2 || pressure != 2 {
		t.Fatalf("all history: rate=%d pressure=%d err=%v", rate, pressure, err)
	}
	var got [][3]int64
	err = EachDeviceAlertCount(ctx, tx, func(device *DeviceAlertState, rate, pressure int64) {
		got = append(got, [3]int64{device.DeviceID, rate, pressure})
	})
	if err != nil || len(got) != 2 || got[0] != [3]int64{1, 2, 1} || got[1] != [3]int64{2, 0, 0} {
		t.Fatalf("active devices: %v err=%v", got, err)
	}
}

// Compare throughput on the same migrated MySQL database and mixed alert history.
func BenchmarkPendingCountsMySQL(b *testing.B) {
	pool := dbtest.MySQL(b)
	ctx := context.Background()
	_, err := pool.ExecContext(ctx, `INSERT INTO device(device_id,device_no,secret)
		WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<256)
		SELECT n, CONCAT('D',n), 'bench' FROM seq`)
	if err != nil {
		b.Fatal(err)
	}
	_, err = pool.ExecContext(ctx, `INSERT INTO ratepre(device_id,rate_state,pre_state,create_time)
		WITH RECURSIVE seq(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM seq WHERE n<255)
		SELECT a.n+1, IF(a.n%25=0,'1','0'), IF(b.n%40=0,'1','0'), '2026-01-02'
		FROM seq a CROSS JOIN seq b`)
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct{ name, query string }{
		{"separate", `SELECT (SELECT COUNT(*) FROM ratepre r WHERE ` + pendingRate +
			`), (SELECT COUNT(*) FROM ratepre r WHERE ` + pendingPressure + `)`},
		{"combined", pendingCountsSQL},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var rate, pressure int64
					err := pool.QueryRowContext(ctx, tc.query).Scan(&rate, &pressure)
					if err != nil || rate != 2816 || pressure != 1792 {
						b.Errorf("rate=%d pressure=%d err=%v", rate, pressure, err)
						return
					}
				}
			})
		})
	}
	const separateDevices = `SELECT ` + personCols + `,
		(SELECT COUNT(*) FROM ratepre r WHERE r.device_id=d.device_id AND ` + pendingRate + `),
		(SELECT COUNT(*) FROM ratepre r WHERE r.device_id=d.device_id AND ` + pendingPressure + `) ` +
		personFrom + ` WHERE d.status=1 ORDER BY d.device_id`
	for _, tc := range []struct{ name, query string }{
		{"devices/separate", separateDevices}, {"devices/combined", deviceAlertCountsSQL},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					var devices, rate, pressure int64
					err := db.QueryEach(ctx, pool, func(rows *sql.Rows) error {
						var hr, bp int64
						_, err := scanPersonRow(rows, &hr, &bp)
						devices++
						rate += hr
						pressure += bp
						return err
					}, tc.query)
					if err != nil || devices != 256 || rate != 2816 || pressure != 1792 {
						b.Errorf("devices=%d rate=%d pressure=%d err=%v", devices, rate, pressure, err)
						return
					}
				}
			})
		})
	}
}

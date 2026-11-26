package rules

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestMissingActivityDoesNotScanOtherMetrics(t *testing.T) {
	p, now := seed(t)
	ctx := context.Background()
	// Many real rows of another metric: a missing steps reading must not scan
	// the owner's history while the rule evaluator holds the device lock.
	digits := `(SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)`
	_, err := p.ExecContext(ctx, `INSERT INTO health_sample(sample_id,old_id,device_id,epoch,version,revision,payload,source,source_device,metric,semantics,sample_start,sample_end,deleted)
 SELECT SHA2(CONCAT('plan',a.n,b.n,c.n),256),1,1,1,1,1+a.n*100+b.n*10+c.n,'{}','watch','RULE001','heart_rate','instant',?,?,false
 FROM `+digits+` a CROSS JOIN `+digits+` b CROSS JOIN `+digits+` c`, now.UnixMicro(), now.UnixMicro())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "ANALYZE TABLE health_sample"); err != nil {
		t.Fatal(err)
	}
	var id, selectType, table, partitions, access, possible, length, reference, filtered, extra any
	var key sql.NullString
	var rows int64
	err = p.QueryRowContext(ctx, "EXPLAIN "+latestSampleSQL, 1, 1, 1, "steps", now.Add(-15*time.Minute).UnixMicro(), now.UnixMicro()).Scan(&id, &selectType, &table, &partitions, &access, &possible, &key, &length, &reference, &rows, &filtered, &extra)
	if err != nil {
		t.Fatal(err)
	}
	if key.String != "idx_health_rule_window" || rows > 10 {
		t.Fatalf("missing metric scans unrelated history: key=%s estimated_rows=%d", key.String, rows)
	}
}

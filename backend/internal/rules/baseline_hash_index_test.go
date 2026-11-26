package rules

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"yihe-care/internal/db"
)

func TestLongBaselineKeyBoundsSharedPrefixCandidates(t *testing.T) {
	p, now := seed(t)
	ctx := t.Context()
	digits := "(SELECT 0 n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)"
	_, err := p.ExecContext(ctx, "INSERT INTO health_sample(sample_id,old_id,device_id,epoch,version,revision,payload,source,source_device,metric,semantics,sample_start,sample_end,deleted) SELECT UNHEX(SHA2(CONCAT('long',a.n,b.n,c.n),256)),1,1,1,1,1+a.n*100+b.n*10+c.n,'{}','watch',CONCAT(REPEAT('d',64),a.n,b.n,c.n),'heart_rate','instant',?,?,false FROM "+digits+" a CROSS JOIN "+digits+" b CROSS JOIN "+digits+" c", now.UnixMicro(), now.UnixMicro())
	if err != nil {
		t.Fatal(err)
	}
	identity := strings.Repeat("d", 64) + "123"
	count := func(column string, key []byte, want int) {
		t.Helper()
		var n int
		query := fmt.Sprintf("SELECT COUNT(*) FROM health_sample FORCE INDEX(idx_health_rule_cover) WHERE old_id=1 AND device_id=1 AND metric='heart_rate' AND deleted=false AND payload_encrypted=false AND source='watch' AND %s=?", column)
		if err := p.QueryRowContext(ctx, query, key).Scan(&n); err != nil || n != want {
			t.Fatalf("candidates=%d want=%d err=%v", n, want, err)
		}
	}
	count("rule_identity_key", baselineSourceKey(identity), 1)
	rollback, err := os.ReadFile("../../migrations/rollback/mysql_0049_health_rule_identity_hash.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(rollback), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := p.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	count("rule_source_key", append([]byte{1}, identity[:64]...), 1000)
	if err := db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal(err)
	}
	count("rule_identity_key", baselineSourceKey(identity), 1)
}

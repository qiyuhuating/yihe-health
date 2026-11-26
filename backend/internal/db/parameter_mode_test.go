package db

import (
	"strings"
	"testing"
)

func TestMySQLParameterModeConfiguration(t *testing.T) {
	for _, tc := range []struct {
		dsn  string
		want bool
		fail bool
	}{
		{"u:p@tcp(localhost:3306)/synthetic", true, false},
		{"u:p@tcp(localhost:3306)/synthetic?interpolateParams=false", false, false},
		{"u:p@tcp(localhost:3306)/synthetic?interpolateParams=true", true, false},
		{"u:interpolateParams=false?@tcp(localhost:3306)/synthetic", true, false},
		{"u:p@unix(/tmp/mysql.sock)/synthetic?parseTime=true&interpolateParams=false", false, false},
		{"u:p@tcp(localhost:3306)/synthetic?interpolateParams=bad", false, true},
		{"u:p@tcp(localhost:3306)/synthetic?collation=gbk_chinese_ci", false, true},
		{"u:p@tcp(localhost:3306)/synthetic?collation=gbk_chinese_ci&interpolateParams=false", false, false},
	} {
		cfg, err := mysqlConnectionConfig(tc.dsn)
		if (err != nil) != tc.fail {
			t.Fatalf("mode config error=%v want failure=%v", err, tc.fail)
		}
		if err == nil && (cfg.Params["transaction_isolation"] != "'READ-COMMITTED'" || cfg.InterpolateParams != tc.want || cfg.MultiStatements || !strings.Contains(cfg.FormatDSN(), "charset=utf8mb4")) {
			t.Fatal("parameter transport options not preserved")
		}
	}
}

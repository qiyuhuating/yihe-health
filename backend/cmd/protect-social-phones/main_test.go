package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/socialrepo"
)

func TestSocialPhoneCommandHelpAndBounds(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--help"}, &out); err != nil || !strings.Contains(out.String(), "1..1000") {
		t.Fatal("help requires database configuration")
	}
	for _, args := range [][]string{{"--limit", "0"}, {"--limit", "1001"}, {"--limit", "invalid"}, {"--unknown"}, {"unexpected"}} {
		out.Reset()
		if err := run(args, &out); err == nil || out.Len() != 0 {
			t.Fatal("invalid options emitted a misleading report")
		}
	}
}

func TestSocialPhoneCommandMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	var database string
	if err := pool.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	cfg.DBName = database
	dir := t.TempDir()
	write := func(name, value string) string {
		t.Helper()
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("MYSQL_DSN_FILE", write("dsn", cfg.FormatDSN()))
	t.Setenv("DATA_ENCRYPTION_KEY", "")
	t.Setenv("DATA_ENCRYPTION_KEY_FILE", write("key", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))))
	if _, err := pool.ExecContext(ctx, "INSERT INTO said(said_phone) VALUES('13800000001'),('13800000002')"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	read := func() socialrepo.PhoneBackfillReport {
		t.Helper()
		var report socialrepo.PhoneBackfillReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatal("missing aggregate JSON")
		}
		for _, secret := range []string{"13800000001", "13800000002", cfg.FormatDSN()} {
			if strings.Contains(out.String(), secret) {
				t.Fatal("command report exposed input")
			}
		}
		return report
	}
	if err := run(nil, &out); err != nil || read().Migrated != 0 || read().Before != 2 {
		t.Fatal("scan changed rows", err)
	}
	out.Reset()
	if err := run([]string{"--apply", "--limit", "1"}, &out); !errors.Is(err, socialrepo.ErrPhoneBackfillIncomplete) || read().Complete || read().Migrated != 1 || read().Remaining == nil || *read().Remaining != 1 {
		t.Fatal("bounded command hid partial completion", err)
	}
	out.Reset()
	if err := run([]string{"--apply"}, &out); err != nil || !read().Complete || read().Migrated != 1 {
		t.Fatal("command could not resume", err)
	}
	out.Reset()
	if err := run([]string{"--apply"}, &out); err != nil || !read().Complete || read().Migrated != 0 {
		t.Fatal("command was not idempotent", err)
	}
	var plaintext, protected int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(said_phone),SUM(said_phone_protected) FROM said").Scan(&plaintext, &protected); err != nil || plaintext != 0 || protected != 2 {
		t.Fatal("command left plaintext primary phones", err)
	}
}

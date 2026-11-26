package db

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	rollbackRefPattern  = regexp.MustCompile(`(?i)(?:DELETE|SELECT\s+name)\s+FROM\s+schema_migrations\s+WHERE\s+name\s*=\s*'([^']+)'`)
	migrationNumPattern = regexp.MustCompile(`^(\d{4})_`)
)

// The rollback coverage gate applies to MySQL business migrations only.
// Retired PostgreSQL projections are historical recovery assets only.
func TestMigrationRollbackPairs(t *testing.T) {
	names, err := MySQLMigrationNames()
	if err != nil {
		t.Fatal(err)
	}
	forward, covered := nameSet(names), make(map[string]bool)
	numbered := make(map[int]string, len(names))
	for _, name := range names {
		n, ok := migrationNumber(path.Base(name))
		if !ok {
			t.Fatalf("MySQL migration %s has no numeric prefix", name)
		}
		if previous, exists := numbered[n]; exists {
			t.Fatalf("duplicate MySQL migration number %04d: %s and %s", n, previous, name)
		}
		numbered[n] = name
	}
	dir := filepath.Join("..", "..", "migrations", "rollback")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".sql") {
			continue
		}
		if !strings.HasPrefix(fileName, "mysql_") {
			t.Errorf("unexpected rollback script %q: only MySQL business migrations are supported", fileName)
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, fileName))
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "--") && strings.Contains(trimmed, ";") {
				t.Errorf("%s: a semicolon inside a comment is split into a statement by MigrateMySQL", fileName)
			}
		}
		matches := rollbackRefPattern.FindAllStringSubmatch(body, -1)
		if len(matches) != 1 {
			t.Errorf("%s: expected one schema_migrations delete or retained-ledger query, found %d", fileName, len(matches))
			continue
		}
		ref := matches[0][1]
		if !forward[ref] {
			t.Errorf("%s references unknown MySQL migration %q", fileName, ref)
			continue
		}
		if expected := "mysql_" + path.Base(ref); fileName != expected {
			t.Errorf("%s should be named %s to match %q", fileName, expected, ref)
		}
		if covered[ref] {
			t.Errorf("%s duplicates the rollback for %q", fileName, ref)
		}
		covered[ref] = true
	}
	numbers := make([]int, 0, len(covered))
	for name := range covered {
		n, ok := migrationNumber(path.Base(name))
		if !ok {
			t.Fatalf("covered migration %q has no numeric prefix", name)
		}
		numbers = append(numbers, n)
	}
	if len(numbers) == 0 {
		t.Fatal("no MySQL rollback scripts found")
	}
	sort.Ints(numbers)
	lowest := numbers[0]
	for _, name := range names {
		n, ok := migrationNumber(path.Base(name))
		if ok && n >= lowest && !covered[name] {
			t.Errorf("MySQL migration %s has no rollback script", name)
		}
	}
}

func nameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

func migrationNumber(fileName string) (int, bool) {
	m := migrationNumPattern.FindStringSubmatch(fileName)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

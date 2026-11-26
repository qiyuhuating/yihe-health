package dbtest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrepareSharedMySQLFixture(t *testing.T) {
	path := os.Getenv(exportFixtureEnv)
	if path == "" {
		t.Skip("opt-in shared fixture preparation")
	}
	if os.Getenv(sharedFixtureEnv) != "" {
		t.Fatal("fixture preparation cannot consume a shared archive")
	}
	pool := MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	name, server := fixtureIdentity(t, ctx, pool)
	archive := dumpMySQL(t, ctx, name, server, true)
	if err := saveSharedFixture(path, server, archive); err != nil {
		t.Fatal(err)
	}
}

func TestSharedFixtureIdentityAndIntegrity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.json")
	archive := []byte("synthetic pristine schema")
	if err := saveSharedFixture(path, "server-a", archive); err != nil {
		t.Fatal(err)
	}
	if got, err := loadSharedFixture(path, "server-a"); err != nil || string(got) != string(archive) {
		t.Fatalf("load: %s %v", got, err)
	}
	if _, err := loadSharedFixture(path, "server-b"); err == nil {
		t.Fatal("accepted a foreign server")
	}
	if err := saveSharedFixture(path, "server-a", archive); err == nil {
		t.Fatal("overwrote an existing archive")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value sharedFixture
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*sharedFixture)
	}{
		{"stale migration", func(v *sharedFixture) { v.Migrations[0] ^= 1 }},
		{"corrupted archive", func(v *sharedFixture) { v.Archive = []byte("changed") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := value
			tc.mutate(&changed)
			data, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(t.TempDir(), "fixture.json")
			if err = os.WriteFile(name, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = loadSharedFixture(name, "server-a"); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
}

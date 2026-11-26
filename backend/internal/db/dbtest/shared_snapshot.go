package dbtest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"yihe-care/internal/db"
	"yihe-care/migrations"
)

const sharedFixtureEnv = "TEST_MYSQL_SHARED_FIXTURE"
const exportFixtureEnv = "TEST_MYSQL_EXPORT_FIXTURE"

// The archive is generated from an empty, fully migrated test database,
// never from a database that has been exposed to a business test.
type sharedFixture struct {
	Server     string
	Migrations [32]byte
	Checksum   [32]byte
	Archive    []byte
}

func migrationFingerprint() ([32]byte, error) {
	var result [32]byte
	names, err := db.MySQLMigrationNames()
	if err != nil {
		return result, err
	}
	hash := sha256.New()
	for _, name := range names {
		data, err := migrations.Files.ReadFile(name)
		if err != nil {
			return result, err
		}
		// A fixed-length digest per file, together with its name, makes changes
		// to either migration order or contents invalidate the fixture.
		fmt.Fprintf(hash, "%d:%s", len(name), name)
		sum := sha256.Sum256(data)
		hash.Write(sum[:])
	}
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func saveSharedFixture(path, server string, archive []byte) error {
	fingerprint, err := migrationFingerprint()
	if err != nil {
		return err
	}
	value := sharedFixture{Server: server, Migrations: fingerprint, Checksum: sha256.Sum256(archive), Archive: archive}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(value)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func loadSharedFixture(path, server string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value sharedFixture
	if err = json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	fingerprint, err := migrationFingerprint()
	if err != nil {
		return nil, err
	}
	if value.Server != server {
		return nil, fmt.Errorf("shared fixture belongs to a different MySQL server")
	}
	if value.Migrations != fingerprint {
		return nil, fmt.Errorf("shared fixture migration fingerprint is stale")
	}
	if len(value.Archive) == 0 || value.Checksum != sha256.Sum256(value.Archive) {
		return nil, fmt.Errorf("shared fixture archive checksum mismatch")
	}
	return value.Archive, nil
}

// Fixture is for ordinary business tests. Migration, rollback and backup
// tests should explicitly retain MySQL, even during shared fixture runs.
func Fixture(t testing.TB) *db.Database {
	t.Helper()
	if os.Getenv(sharedFixtureEnv) != "" || os.Getenv("TEST_MYSQL_FIXTURE_SNAPSHOT") == "1" {
		return MySQLSnapshot(t)
	}
	return MySQL(t)
}

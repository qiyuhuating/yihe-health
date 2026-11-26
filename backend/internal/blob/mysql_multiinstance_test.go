package blob

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

// Two separately pooled application instances must agree on immutable objects;
// a restored MySQL catalog and payload must authenticate with the original key.
func TestMySQLImageTwoInstancesAndRestoredPayload(t *testing.T) {
	primary := dbtest.MySQL(t)
	restored := dbtest.MySQL(t)
	ctx := context.Background()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	box, err := securestore.NewKeyring("current", map[string]string{"current": key}, key)
	if err != nil {
		t.Fatal(err)
	}
	var databaseName string
	if err := primary.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = databaseName
	peer, err := db.OpenMySQL(ctx, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	rawA, err := NewMySQL(primary.DB)
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := NewMySQL(peer.DB)
	if err != nil {
		t.Fatal(err)
	}
	first := &Manager{DB: primary, Store: &Encrypted{Store: rawA, Box: box}}
	second := &Manager{DB: peer, Store: &Encrypted{Store: rawB, Box: box}}
	content := bytes.Repeat([]byte{0x7d}, MaxPlainBytes)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, manager := range []*Manager{first, second} {
		wg.Add(1)
		go func(m *Manager) {
			defer wg.Done()
			<-start
			results <- m.Put(ctx, "shared.png", content)
		}(manager)
	}
	close(start)
	wg.Wait()
	close(results)
	inserted, duplicates := 0, 0
	for result := range results {
		switch {
		case result == nil:
			inserted++
		case errors.Is(result, ErrExists):
			duplicates++
		default:
			t.Fatalf("unexpected concurrent insert error: %v", result)
		}
	}
	if inserted != 1 || duplicates != 1 {
		t.Fatalf("competing instances published %d objects, rejected %d duplicates", inserted, duplicates)
	}
	if got, err := second.Get(ctx, "shared.png"); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("second instance could not authenticate shared object: %v", err)
	}
	var payload []byte
	if err := primary.QueryRowContext(ctx, "SELECT payload FROM image_payload WHERE name='shared.png'").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, content) {
		t.Fatal("MySQL payload exposed plaintext")
	}
	var digest string
	var size int64
	if err := primary.QueryRowContext(ctx, "SELECT content_sha256,size_bytes FROM image_blob WHERE name='shared.png'").Scan(&digest, &size); err != nil {
		t.Fatal(err)
	}
	tx, err := restored.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO image_payload(name,payload) VALUES(?,?)", "shared.png", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO image_blob(name,content_sha256,size_bytes,state) VALUES(?,?,?,'ready')", "shared.png", digest, size); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	restoredRaw, err := NewMySQL(restored.DB)
	if err != nil {
		t.Fatal(err)
	}
	reopened := &Manager{DB: restored, Store: &Encrypted{Store: restoredRaw, Box: box}}
	if got, err := reopened.Get(ctx, "shared.png"); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("restored image does not match original: %v", err)
	}
	if checked, err := reopened.VerifyReadyCatalog(ctx); err != nil || checked != 1 {
		t.Fatalf("restored image catalog failed authentication: count=%d err=%v", checked, err)
	}
}

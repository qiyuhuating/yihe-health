package lane

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"yihe-care/internal/blob"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func TestS3ShadowMigration(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_S3_ENDPOINT not set")
	}
	access, secret := os.Getenv("TEST_S3_ACCESS_KEY"), os.Getenv("TEST_S3_SECRET_KEY")
	if access == "" || secret == "" {
		t.Fatal("missing isolated S3 credentials")
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: false, BucketLookup: minio.BucketLookupPath, Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	bucket := "yihe-test-" + uuid.NewString()
	if err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for obj := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err != nil {
				t.Error(obj.Err)
				break
			}
			if err := client.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				t.Error(err)
			}
		}
		if err := client.RemoveBucket(ctx, bucket); err != nil {
			t.Error(err)
		}
	})
	db := pool.DB
	sinkExec(t, db, "CREATE TABLE shadow_image_object(name VARCHAR(255) PRIMARY KEY,object_key VARCHAR(71) NOT NULL,cipher_size BIGINT NOT NULL,plain_sha256 CHAR(64) NOT NULL,plain_size BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin")
	key := func(v byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{v}, 32)) }
	old, err := securestore.NewKeyring("A", map[string]string{"A": key(1)}, key(8))
	if err != nil {
		t.Fatal(err)
	}
	dual, err := securestore.NewKeyring("B", map[string]string{"A": key(1), "B": key(2)}, key(8))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := blob.NewMySQL(db)
	if err != nil {
		t.Fatal(err)
	}
	manager := &blob.Manager{DB: pool, Store: &blob.Encrypted{Store: raw, Box: old}}
	const name = "synthetic.png"
	plain := bytes.Repeat([]byte{0x73}, blob.MaxPlainBytes)
	if err = manager.Put(ctx, name, plain); err != nil {
		t.Fatal(err)
	}
	sinkExec(t, db, "INSERT INTO pic(pic_id,pic_name) VALUES(1,?)", name)
	m := S3Migration{DB: db, Box: dual, Client: client, Bucket: bucket}
	db.SetMaxOpenConns(1)

	// Failure after PUT must leave the source intact and no published reference.
	sinkExec(t, db, "CREATE TRIGGER shadow_fail BEFORE INSERT ON shadow_image_object FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected ledger failure'")
	if err = m.Copy(ctx, name); err == nil {
		t.Fatal("injected failure ignored")
	}
	sinkCount(t, db, "SELECT COUNT(*) FROM shadow_image_object", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM image_payload", 1)
	snapshot, err := m.snapshot(ctx, db, name)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.verifyObject(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	sinkExec(t, db, "DROP TRIGGER shadow_fail")
	count, err := m.CopyReady(ctx, 1)
	if err != nil || count != 1 {
		t.Fatalf("resume: %d %v", count, err)
	}
	if err = m.Copy(ctx, name); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err = m.Verify(ctx, name); err != nil {
		t.Fatal(err)
	}
	sinkCount(t, db, "SELECT COUNT(*) FROM shadow_image_object", 1)

	// The real rotation path changes active generation but not the logical name.
	report, err := blob.RotateMySQLImages(ctx, db, dual, 1, true)
	if err != nil || report.Rewritten != 1 {
		t.Fatalf("rotation: %v %v", report, err)
	}
	if err = m.publishReference(ctx, snapshot); !errors.Is(err, ErrImageSourceChanged) {
		t.Fatalf("stale CAS: %v", err)
	}
	if err = m.Verify(ctx, name); !errors.Is(err, blob.ErrIntegrity) {
		t.Fatalf("stale reference: %v", err)
	}
	if err = m.Copy(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, name); err != nil {
		t.Fatal(err)
	}
	rotated, err := m.snapshot(ctx, db, name)
	if err != nil || rotated.objectKey() == snapshot.objectKey() {
		t.Fatalf("rotated object identity: %v", err)
	}

	// A collision or remote corruption must never be silently overwritten.
	bad := []byte("corrupt remote bytes")
	if _, err = client.PutObject(ctx, bucket, rotated.objectKey(), bytes.NewReader(bad), int64(len(bad)), minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, name); !errors.Is(err, blob.ErrIntegrity) {
		t.Fatalf("remote corruption: %v", err)
	}
	if err = m.Copy(ctx, name); err == nil {
		t.Fatal("corrupt object overwritten")
	}
	obj, err := client.StatObject(ctx, bucket, rotated.objectKey(), minio.StatObjectOptions{})
	if err != nil || obj.Size != int64(len(bad)) {
		t.Fatalf("conditional PUT failed to protect object: %v %v", obj.Size, err)
	}
	sinkCount(t, db, "SELECT COUNT(*) FROM image_payload", 1)

	// Missing business references block a bounded catalog run before any copy.
	sinkExec(t, db, "INSERT INTO pic(pic_id,pic_name) VALUES(2,'missing.png')")
	if count, err = m.CopyReady(ctx, 2); err == nil || count != 0 {
		t.Fatalf("missing references: %d %v", count, err)
	}
}

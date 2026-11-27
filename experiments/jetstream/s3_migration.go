package lane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"yihe-care/internal/blob"
	"yihe-care/internal/securestore"
)

var ErrImageSourceChanged = errors.New("image source changed during shadow copy")

// S3Migration performs non-destructive shadow copies. MySQL remains the
// authority, and neither source payloads nor remote objects are deleted.
type S3Migration struct {
	DB     *sql.DB
	Box    *securestore.Box
	Client *minio.Client
	Bucket string
}

type imageCopy struct {
	name, digest           string
	size                   int64
	generation, ciphertext []byte
}

func (s imageCopy) objectKey() string {
	sum := sha256.Sum256(s.ciphertext)
	return "images/" + hex.EncodeToString(sum[:])
}

type imageReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (m S3Migration) snapshot(ctx context.Context, db imageReader, name string) (imageCopy, error) {
	s := imageCopy{name: name}
	if !blob.ValidKey(name) || m.Box == nil {
		return s, blob.ErrIntegrity
	}
	// Read catalog and active ciphertext in a single consistent statement.
	err := db.QueryRowContext(ctx, `SELECT c.content_sha256,c.size_bytes,p.active_generation,
		CASE WHEN OCTET_LENGTH(COALESCE(p.payload,g.payload))<=? THEN COALESCE(p.payload,g.payload) END
		FROM image_blob c JOIN image_payload p ON p.name=c.name
		LEFT JOIN image_payload_generation g ON g.name=p.name AND g.generation=p.active_generation
		WHERE c.name=? AND c.state='ready'`, blob.MaxStoredBytes, name).
		Scan(&s.digest, &s.size, &s.generation, &s.ciphertext)
	if err != nil {
		return s, err
	}
	if err := m.authenticate(s, s.ciphertext); err != nil {
		return s, err
	}
	return s, nil
}

func (m S3Migration) authenticate(s imageCopy, ciphertext []byte) error {
	if len(ciphertext) == 0 || len(ciphertext) > blob.MaxStoredBytes || !bytes.HasPrefix(ciphertext, []byte(securestore.Prefix)) {
		return blob.ErrIntegrity
	}
	plain, err := m.Box.OpenEncodedBytes(ciphertext, "image:"+s.name)
	if err != nil || len(plain) > blob.MaxPlainBytes || int64(len(plain)) != s.size || blob.Digest(plain) != s.digest {
		return blob.ErrIntegrity
	}
	return nil
}

func (m S3Migration) verifyObject(ctx context.Context, s imageCopy) error {
	obj, err := m.Client.GetObject(ctx, m.Bucket, s.objectKey(), minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer obj.Close()
	data, err := io.ReadAll(io.LimitReader(obj, blob.MaxStoredBytes+1))
	if err != nil {
		return err
	}
	// Compare bytes, not the S3 ETag (which need not be a content digest).
	if !bytes.Equal(data, s.ciphertext) {
		return blob.ErrIntegrity
	}
	return m.authenticate(s, data)
}

func (m S3Migration) upload(ctx context.Context, s imageCopy) error {
	opts := minio.PutObjectOptions{ContentType: "application/octet-stream", DisableMultipart: true, SendContentMd5: true}
	opts.SetMatchETagExcept("*")
	_, err := m.Client.PutObject(ctx, m.Bucket, s.objectKey(), bytes.NewReader(s.ciphertext), int64(len(s.ciphertext)), opts)
	if err != nil {
		// A prior attempt may have published the object but lost its response.
		// Do not overwrite it: only a successful exact-byte verification permits
		// progress, regardless of the PUT error.
		if check := m.verifyObject(ctx, s); check != nil {
			return errors.Join(err, check)
		}
		return nil
	}
	return m.verifyObject(ctx, s)
}

func (m S3Migration) publishReference(ctx context.Context, expected imageCopy) error {
	tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Match the production guard/catalog/payload lock order. Never hold a SQL
	// transaction or row lock across S3 requests.
	var backupActive bool
	if err = tx.QueryRowContext(ctx, "SELECT backup_until>CURRENT_TIMESTAMP(6) FROM image_gc_guard WHERE id=1 FOR UPDATE").Scan(&backupActive); err != nil {
		return err
	}
	if backupActive {
		return blob.ErrBusy
	}
	var name string
	for _, query := range []string{
		"SELECT name FROM image_blob WHERE name=? FOR UPDATE",
		"SELECT name FROM image_payload WHERE name=? FOR UPDATE",
	} {
		if err = tx.QueryRowContext(ctx, query, expected.name).Scan(&name); err != nil {
			return err
		}
	}
	current, err := m.snapshot(ctx, tx, expected.name)
	if err != nil {
		return err
	}
	if current.digest != expected.digest || current.size != expected.size ||
		!bytes.Equal(current.generation, expected.generation) || !bytes.Equal(current.ciphertext, expected.ciphertext) {
		return ErrImageSourceChanged
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO shadow_image_object(name,object_key,cipher_size,plain_sha256,plain_size)
		VALUES(?,?,?,?,?) AS new ON DUPLICATE KEY UPDATE object_key=new.object_key,cipher_size=new.cipher_size,plain_sha256=new.plain_sha256,plain_size=new.plain_size`,
		current.name, current.objectKey(), len(current.ciphertext), current.digest, current.size)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (m S3Migration) Copy(ctx context.Context, name string) error {
	s, err := m.snapshot(ctx, m.DB, name)
	if err != nil {
		return err
	}
	if err = m.upload(ctx, s); err != nil {
		return err
	}
	return m.publishReference(ctx, s)
}

// Verify compares the current authoritative snapshot, shadow ledger, and S3
// bytes. This is a shadow verifier, not a production read path or cutover gate.
func (m S3Migration) Verify(ctx context.Context, name string) error {
	s, err := m.snapshot(ctx, m.DB, name)
	if err != nil {
		return err
	}
	var key, digest string
	var cipherSize, plainSize int64
	err = m.DB.QueryRowContext(ctx, "SELECT object_key,cipher_size,plain_sha256,plain_size FROM shadow_image_object WHERE name=?", name).
		Scan(&key, &cipherSize, &digest, &plainSize)
	if err != nil {
		return err
	}
	if key != s.objectKey() || cipherSize != int64(len(s.ciphertext)) || digest != s.digest || plainSize != s.size {
		return blob.ErrIntegrity
	}
	if err = m.verifyObject(ctx, s); err != nil {
		return err
	}
	current, err := m.snapshot(ctx, m.DB, name)
	if err != nil {
		return err
	}
	if current.digest != s.digest || current.size != s.size || !bytes.Equal(current.generation, s.generation) || !bytes.Equal(current.ciphertext, s.ciphertext) {
		return ErrImageSourceChanged
	}
	return nil
}

// CopyReady copies a bounded ready catalog without keeping an SQL cursor
// open across network requests. A stopped run is safely restartable.
func (m S3Migration) CopyReady(ctx context.Context, maxRows int) (int, error) {
	if maxRows <= 0 {
		return 0, errors.New("positive image migration limit required")
	}
	missing, err := blob.MissingMySQLReferences(ctx, m.DB)
	if err != nil {
		return 0, err
	}
	if missing != 0 {
		return 0, fmt.Errorf("%d missing source references", missing)
	}
	var after string
	count := 0
	for {
		rows, err := m.DB.QueryContext(ctx, "SELECT name FROM image_blob WHERE state='ready' AND name>? ORDER BY name LIMIT 128", after)
		if err != nil {
			return count, err
		}
		var names []string
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				break
			}
			names = append(names, name)
		}
		if err == nil {
			err = rows.Err()
		}
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return count, err
		}
		for _, name := range names {
			if count == maxRows {
				return count, errors.New("image migration row limit reached")
			}
			if err = m.Copy(ctx, name); err != nil {
				return count, err
			}
			count++
			after = name
		}
		if len(names) < 128 {
			return count, nil
		}
	}
}

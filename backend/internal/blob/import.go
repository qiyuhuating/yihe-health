package blob

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"yihe-care/internal/securestore"
)

// ImportLegacy atomically imports one previous local/S3-exported image and its
// catalog. A matching retry is a no-op; it never replaces an existing key.
func ImportLegacy(ctx context.Context, database *sql.DB, box *securestore.Box, key string, source []byte) (bool, error) {
	if database == nil || box == nil || !ValidKey(key) {
		return false, errors.New("MySQL database, encryption key and valid image name required")
	}
	if len(source) > MaxStoredBytes {
		return false, ErrIntegrity
	}
	var plain []byte
	if bytes.HasPrefix(source, []byte(securestore.Prefix)) {
		opened, err := box.OpenEncodedBytes(source, "image:"+key)
		if err != nil {
			return false, ErrIntegrity
		}
		plain = opened
	} else {
		plain = source // Older local backends could store plaintext.
	}
	if len(plain) > MaxPlainBytes {
		return false, ErrIntegrity
	}
	digest := Digest(plain)
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var recordedDigest, state string
	var recordedSize int64
	err = tx.QueryRowContext(ctx, "SELECT content_sha256,size_bytes,state FROM image_blob WHERE name=? FOR UPDATE", key).
		Scan(&recordedDigest, &recordedSize, &state)
	needsCatalog := errors.Is(err, sql.ErrNoRows)
	if err != nil && !needsCatalog {
		return false, err
	}
	if !needsCatalog && (state != "ready" || recordedDigest != digest || recordedSize != int64(len(plain))) {
		return false, fmt.Errorf("%w: existing image catalog differs for %q", ErrIntegrity, key)
	}
	existing, err := getPayload(ctx, tx, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if err == nil {
		if !bytes.HasPrefix(existing, []byte(securestore.Prefix)) {
			return false, ErrIntegrity
		}
		opened, openErr := box.OpenEncodedBytes(existing, "image:"+key)
		if openErr != nil || Digest(opened) != digest || len(opened) != len(plain) {
			return false, ErrIntegrity
		}
		if needsCatalog {
			_, err = tx.ExecContext(ctx, "INSERT INTO image_blob(name,content_sha256,size_bytes,state) VALUES(?,?,?,'ready')", key, digest, len(plain))
			if err != nil {
				return false, err
			}
			return true, tx.Commit()
		}
		return false, nil
	}

	sealed, err := box.SealEncodedBytes(plain, "image:"+key)
	if err != nil {
		return false, err
	}
	if len(sealed) > MaxStoredBytes {
		return false, ErrIntegrity
	}
	if needsCatalog {
		if _, err = tx.ExecContext(ctx, "INSERT INTO image_blob(name,content_sha256,size_bytes,state) VALUES(?,?,?,'ready')", key, digest, len(plain)); err != nil {
			return false, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO image_payload(name,payload) VALUES(?,?)", key, sealed); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// MissingMySQLReferences reports references that have not yet been migrated.
// It excludes the built-in kai.jpg fallback. No payload or key material leaves MySQL.
func MissingMySQLReferences(ctx context.Context, database *sql.DB) (int64, error) {
	const query = "SELECT COUNT(*) FROM (" +
		" SELECT family_tou_pic AS name FROM family WHERE family_tou_pic IS NOT NULL AND family_tou_pic<>''" +
		" UNION SELECT tou_pic FROM said WHERE tou_pic IS NOT NULL AND tou_pic<>''" +
		" UNION SELECT pic_name FROM pic WHERE pic_name IS NOT NULL AND pic_name<>''" +
		" UNION SELECT pic_name FROM pic_show WHERE pic_name IS NOT NULL AND pic_name<>''" +
		" UNION SELECT name FROM image_blob WHERE state='ready'" +
		" ) refs LEFT JOIN image_payload p ON p.name=refs.name" +
		activeGenerationJoin +
		" LEFT JOIN image_blob c ON c.name=refs.name AND c.state='ready'" +
		" WHERE (refs.name<>'kai.jpg' OR c.name IS NOT NULL) AND (" + activePayload + " IS NULL OR c.name IS NULL)"
	var missing int64
	err := database.QueryRowContext(ctx, query).Scan(&missing)
	return missing, err
}

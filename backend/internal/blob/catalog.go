package blob

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"yihe-care/internal/db"
)

// The historical default avatar is a shipped asset, available on every host.
// Existing on-disk kai.jpg still takes precedence over this fallback.
//
//go:embed assets/default-avatar.png
var defaultAvatar []byte

var ErrBusy = errors.New("blob maintenance is blocked by a backup lease")

type Manager struct {
	DB    *db.Database
	Store Store
}
type Record struct {
	Key    string `json:"key"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	State  string `json:"state"`
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (m *Manager) rawGet(ctx context.Context, key string) ([]byte, error) {
	data, err := m.Store.Get(ctx, key)
	if errors.Is(err, ErrNotFound) && key == "kai.jpg" {
		return append([]byte(nil), defaultAvatar...), nil
	}
	return data, err
}
func (m *Manager) Get(ctx context.Context, key string) ([]byte, error) {
	var record Record
	if m.DB != nil {
		err := m.DB.QueryRowContext(ctx, "SELECT content_sha256,size_bytes,state FROM image_blob WHERE name=?", key).Scan(&record.SHA256, &record.Size, &record.State)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && record.State != "ready" {
			return nil, ErrNotFound
		}
	}
	data, err := m.rawGet(ctx, key)
	if err != nil {
		return nil, err
	}
	if record.SHA256 != "" && (int64(len(data)) != record.Size || Digest(data) != record.SHA256) {
		return nil, ErrIntegrity
	}
	return data, nil
}
func (m *Manager) Put(ctx context.Context, key string, data []byte) error {
	// Both the encrypted bytes and their integrity catalog reside in MySQL.
	// Commit them together: the older two-store compensation path is retained
	// solely for offline/legacy test backends.
	if encrypted, ok := m.Store.(*Encrypted); ok {
		if _, mysqlOnly := encrypted.Store.(*MySQL); mysqlOnly {
			if m.DB == nil {
				return errors.New("MySQL images require their business catalog")
			}
			return encrypted.putMySQLCatalog(ctx, m.DB.DB, key, data)
		}
	}
	if err := m.Store.Put(ctx, key, data); err != nil {
		return err
	}
	if m.DB == nil {
		return nil
	}
	cleanup := func() {
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = m.Store.Delete(finish, key) // A failed compensation is a private orphan, never a false DB reference.
	}
	tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		cleanup()
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO image_blob(name,content_sha256,size_bytes,state) VALUES(?,?,?,'ready')", key, Digest(data), len(data))
	if err != nil {
		_ = tx.Rollback()
		cleanup()
		return err
	}
	// A lost COMMIT acknowledgement is ambiguous. Retain the immutable object:
	// deleting it could break a transaction that the server actually committed.
	return tx.Commit()
}

// Require serializes reference creation with GC using catalog row locks. Every
// business writer must call it before adding an image reference, in the same tx.
func (m *Manager) Require(ctx context.Context, tx *sql.Tx, keys []string) error {
	keys = append([]string(nil), keys...)
	sort.Strings(keys)
	for i, key := range keys {
		if i > 0 && key == keys[i-1] {
			continue
		}
		if !ValidKey(key) {
			return ErrNotFound
		}
		var data []byte
		var err error
		if transactional, ok := m.Store.(interface {
			GetTx(context.Context, *sql.Tx, string) ([]byte, error)
		}); ok {
			data, err = transactional.GetTx(ctx, tx, key)
			if errors.Is(err, ErrNotFound) && key == "kai.jpg" {
				data, err = append([]byte(nil), defaultAvatar...), nil
			}
		} else {
			data, err = m.rawGet(ctx, key)
		}
		if err != nil {
			return err
		}
		// Adopt existing local images without changing their bytes. An atomic
		// insert and subsequent current read also serialize concurrent adopters.
		_, err = tx.ExecContext(ctx, "INSERT INTO image_blob(name,content_sha256,size_bytes,state) VALUES(?,?,?,'ready') ON DUPLICATE KEY UPDATE name=name", key, Digest(data), len(data))
		if err != nil {
			return err
		}
		var digest, state string
		var size int64
		if err = tx.QueryRowContext(ctx, "SELECT content_sha256,size_bytes,state FROM image_blob WHERE name=? FOR UPDATE", key).Scan(&digest, &size, &state); err != nil {
			return err
		}
		if state != "ready" {
			return ErrNotFound
		}
		if digest != Digest(data) || size != int64(len(data)) {
			return ErrIntegrity
		}
	}
	return nil
}

const referenceSQL = `SELECT
 (SELECT count(*) FROM family WHERE family_tou_pic=?)+
 (SELECT count(*) FROM said WHERE tou_pic=?)+
 (SELECT count(*) FROM pic WHERE pic_name=?)+
 (SELECT count(*) FROM pic_show WHERE pic_name=?)`

func (m *Manager) Collect(ctx context.Context, olderThan time.Time, dryRun bool) (int, error) {
	if m.DB == nil {
		return 0, errors.New("GC requires the shared business catalog")
	}
	count := 0
	err := m.Store.Walk(ctx, func(entry Entry) error {
		if entry.Key == "kai.jpg" || !imageKey(entry.Key) || !entry.Modified.Before(olderThan) {
			return nil
		}
		// Verify ownership/authenticity before registering any orphan candidate.
		data, err := m.Store.Get(ctx, entry.Key)
		if err != nil {
			return err
		}
		if dryRun {
			var references int
			if err = m.DB.QueryRowContext(ctx, referenceSQL, entry.Key, entry.Key, entry.Key, entry.Key).Scan(&references); err != nil {
				return err
			}
			if references == 0 {
				count++
			}
			return nil
		}
		tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var backupActive bool
		if err = tx.QueryRowContext(ctx, "SELECT backup_until>CURRENT_TIMESTAMP(6) FROM image_gc_guard WHERE id=1 FOR UPDATE").Scan(&backupActive); err != nil {
			return err
		}
		if backupActive {
			return ErrBusy
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO image_blob(name,content_sha256,size_bytes,state,created_at) VALUES(?,?,?,'ready',?) ON DUPLICATE KEY UPDATE name=name", entry.Key, Digest(data), len(data), entry.Modified)
		if err != nil {
			return err
		}
		var state string
		var created time.Time
		if err = tx.QueryRowContext(ctx, "SELECT state,created_at FROM image_blob WHERE name=? FOR UPDATE", entry.Key).Scan(&state, &created); err != nil {
			return err
		}
		if !created.Before(olderThan) {
			return nil
		}
		var references int
		if err = tx.QueryRowContext(ctx, referenceSQL, entry.Key, entry.Key, entry.Key, entry.Key).Scan(&references); err != nil {
			return err
		}
		if references != 0 {
			return nil
		}
		if _, err = tx.ExecContext(ctx, "UPDATE image_blob SET state='deleting' WHERE name=?", entry.Key); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		// Publish a durable tombstone BEFORE deleting bytes. Reference writers
		// now reject the key, even if this process crashes or the DB disappears.
		if err = m.Store.Delete(ctx, entry.Key); err != nil {
			return err
		}
		if _, err = m.DB.ExecContext(ctx, "UPDATE image_blob SET state='deleted' WHERE name=? AND state='deleting'", entry.Key); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func imageKey(key string) bool {
	if !ValidKey(key) {
		return false
	}
	key = strings.ToLower(key)
	return strings.HasSuffix(key, ".png") || strings.HasSuffix(key, ".jpg") || strings.HasSuffix(key, ".jpeg") || strings.HasSuffix(key, ".bmp")
}

// AcquireBackup prevents GC while an immutable blob superset is copied after
// a consistent MySQL snapshot. Renew before expiry; a failed renewal invalidates
// the backup. Never include encryption keys inside the backup archive.
func (m *Manager) AcquireBackup(ctx context.Context, owner string, seconds int) error {
	if owner == "" || seconds < 60 || seconds > 3600 {
		return errors.New("invalid backup lease")
	}
	tx, err := m.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	var active bool
	var serverNow time.Time
	if err = tx.QueryRowContext(ctx, "SELECT backup_owner,backup_until>CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6) FROM image_gc_guard WHERE id=1 FOR UPDATE").Scan(&current, &active, &serverNow); err != nil {
		return err
	}
	if active && current != owner {
		return ErrBusy
	}
	if _, err = tx.ExecContext(ctx, "UPDATE image_gc_guard SET backup_owner=?,backup_until=? WHERE id=1", owner, serverNow.Add(time.Duration(seconds)*time.Second)); err != nil {
		return err
	}
	return tx.Commit()
}
func (m *Manager) ReleaseBackup(ctx context.Context, owner string) error {
	_, err := m.DB.ExecContext(ctx, "UPDATE image_gc_guard SET backup_owner='',backup_until=CURRENT_TIMESTAMP(6) WHERE id=1 AND backup_owner=?", owner)
	return err
}

func (m *Manager) VerifyReferences(ctx context.Context) ([]Record, error) {
	keys, err := db.QueryList(ctx, m.DB, db.ScanValue[string], `SELECT family_tou_pic AS name FROM family WHERE family_tou_pic IS NOT NULL AND family_tou_pic<>''
 UNION SELECT tou_pic FROM said WHERE tou_pic IS NOT NULL AND tou_pic<>''
 UNION SELECT pic_name FROM pic WHERE pic_name IS NOT NULL AND pic_name<>''
 UNION SELECT pic_name FROM pic_show WHERE pic_name IS NOT NULL AND pic_name<>''`)
	if err != nil {
		return nil, err
	}
	var records []Record
	for _, key := range keys {
		data, err := m.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		records = append(records, Record{key, Digest(data), int64(len(data)), "referenced"})
	}
	return records, nil
}

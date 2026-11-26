package blob

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func TestMySQLImageCiphertextRotation(t *testing.T) {
	db := dbtest.MySQL(t)
	ctx := context.Background()
	key := func(v byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{v}, 32)) }
	ring := func(active string, readers map[string]string) *securestore.Box {
		t.Helper()
		box, err := securestore.NewKeyring(active, readers, key(8))
		if err != nil {
			t.Fatal(err)
		}
		return box
	}
	old := ring("A", map[string]string{"A": key(1)})
	dual := ring("B", map[string]string{"A": key(1), "B": key(2)})
	current := ring("B", map[string]string{"B": key(2)})
	third := ring("C", map[string]string{"B": key(2), "C": key(3)})
	legacy, err := securestore.New(key(1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := NewMySQL(db.DB)
	if err != nil {
		t.Fatal(err)
	}
	// One connection covers cursor closure, transaction-scoped reads and CAS.
	db.DB.SetMaxOpenConns(1)
	manager := func(box *securestore.Box) *Manager {
		return &Manager{DB: db, Store: &Encrypted{Store: raw, Box: box}}
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	get := func(name string) imageSnapshot {
		t.Helper()
		snapshot, err := readImageSnapshot(ctx, db, name)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	const image = "shared.jpg"
	const avatar = "kai.jpg"
	const unused = "unused.png"
	const active = "active.bmp"
	large := bytes.Repeat([]byte{0xa5}, MaxPlainBytes)
	seed := func(box *securestore.Box, name string, data []byte) {
		t.Helper()
		if err := manager(box).Put(ctx, name, data); err != nil {
			t.Fatal(err)
		}
	}
	seed(old, image, large)
	seed(legacy, avatar, []byte("historical custom avatar"))
	seed(old, unused, []byte("unreferenced image"))
	seed(current, active, []byte("new image"))
	exec("INSERT INTO family(family_id,family_tou_pic) VALUES(1,?)", avatar)
	exec("INSERT INTO said(said_id,tou_pic) VALUES(1,?)", image)
	exec("INSERT INTO pic(pic_id,pic_name) VALUES(1,?)", image)
	exec("INSERT INTO pic_show(pic_name) VALUES(?)", active)
	before := get(image)

	report, err := RotateMySQLImages(ctx, db.DB, dual, 4, false)
	if err != nil || !report.Complete || report.Before.Checked != 4 || report.Before.Pending != 3 || report.Rewritten != 0 ||
		!bytes.Equal(before.ciphertext, get(image).ciphertext) {
		t.Fatal("read-only scan lost or changed images", report, err)
	}
	if report, err = RotateMySQLImages(ctx, db.DB, current, 4, true); !errors.Is(err, ErrIntegrity) || report.Rewritten != 0 {
		t.Fatal("missing old reader did not stop every write", report, err)
	}
	if report, err = RotateMySQLImages(ctx, db.DB, dual, 3, true); !errors.Is(err, ErrImageRotationLimit) || report.Rewritten != 0 {
		t.Fatal("incomplete inventory allowed writes", report, err)
	}
	exec("UPDATE image_blob SET size_bytes=999 WHERE name=?", image)
	if report, err = RotateMySQLImages(ctx, db.DB, dual, 4, true); !errors.Is(err, ErrIntegrity) || report.Rewritten != 0 {
		t.Fatal("catalog mismatch did not stop all writes", report, err)
	}
	exec("UPDATE image_blob SET size_bytes=? WHERE name=?", len(large), image)
	m := manager(dual)
	if err := m.AcquireBackup(ctx, "synthetic-backup", 60); err != nil {
		t.Fatal(err)
	}
	if report, err = RotateMySQLImages(ctx, db.DB, dual, 4, true); !errors.Is(err, ErrBusy) || report.Rewritten != 0 {
		t.Fatal("rotation did not respect the backup lease", report, err)
	}
	if err := m.ReleaseBackup(ctx, "synthetic-backup"); err != nil {
		t.Fatal(err)
	}
	report, err = RotateMySQLImages(ctx, db.DB, dual, 4, true)
	if err != nil || !report.Complete || report.Rewritten != 3 || report.After.Checked != 4 || report.After.Pending != 0 ||
		report.RetirementAuthorized {
		t.Fatal("rotation failed to reach a clean live image inventory", report, err)
	}
	rotated := get(image)
	if len(rotated.generation) != 16 || bytes.Equal(before.ciphertext, rotated.ciphertext) || !sameCatalog(before, rotated) {
		t.Fatal("immutable payload version failed to preserve its catalog")
	}
	if err := replaceImageGeneration(ctx, db.DB, image, before, []byte("stale candidate")); !errors.Is(err, ErrImageRotationChanged) ||
		!bytes.Equal(get(image).ciphertext, rotated.ciphertext) {
		t.Fatal("stale rotation candidate replaced the current image", err)
	}
	newReader := manager(current)
	if data, err := newReader.Get(ctx, image); err != nil || !bytes.Equal(data, large) {
		t.Fatal("maximum-size image cannot be read by the new reader", err)
	}
	if data, err := newReader.Get(ctx, avatar); err != nil || string(data) != "historical custom avatar" {
		t.Fatal("custom default avatar changed after rotation", err)
	}
	if _, err := manager(old).Get(ctx, image); !errors.Is(err, ErrIntegrity) {
		t.Fatal("old key alone still opens a rewritten image", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = newReader.Require(ctx, tx, []string{image, image, avatar})
	tx.Rollback()
	if err != nil {
		t.Fatal("normal image attachment failed with a one-connection pool", err)
	}
	if n, err := newReader.VerifyReadyCatalog(ctx); err != nil || n != 4 {
		t.Fatal("rotated catalog did not verify", n, err)
	}
	if records, err := newReader.VerifyReferences(ctx); err != nil || len(records) != 3 {
		t.Fatal("original business references changed", len(records), err)
	}
	if changed, err := ImportLegacy(ctx, db.DB, current, image, large); err != nil || changed {
		t.Fatal("reimport attempted to overwrite a rotated image", changed, err)
	}
	if report, err = RotateMySQLImages(ctx, db.DB, current, 4, true); err != nil || report.Rewritten != 0 || !report.Complete {
		t.Fatal("rerun was not idempotent", report, err)
	}
	if report, err = RotateMySQLImages(ctx, db.DB, third, 4, true); err != nil || report.Rewritten != 4 || !report.Complete {
		t.Fatal("second rotation did not retire previous generations", report, err)
	}
	if bytes.Equal(get(image).generation, rotated.generation) {
		t.Fatal("second rotation overwrote an existing generation")
	}
	var inline, generations int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(payload),(SELECT COUNT(*) FROM image_payload_generation) FROM image_payload").
		Scan(&inline, &generations); err != nil || inline != 0 || generations != 4 {
		t.Fatal("old ciphertext persisted in the live image tables", inline, generations, err)
	}
	thirdReader := manager(third)
	if count, err := thirdReader.Collect(ctx, time.Now().Add(time.Hour), false); err != nil || count != 1 {
		t.Fatal("normal GC did not collect only the unused versioned image", count, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM image_payload_generation WHERE name=?", unused).
		Scan(&generations); err != nil || generations != 0 {
		t.Fatal("versioned GC left detached ciphertext", generations, err)
	}
}

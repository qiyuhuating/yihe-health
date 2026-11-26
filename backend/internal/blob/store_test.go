package blob

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"yihe-care/internal/securestore"
)

func TestLocalEncryptedImmutabilityAndIntegrity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	local, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	box, _ := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	store := &Encrypted{Store: local, Box: box}
	data := []byte("synthetic private image")
	if err = store.Put(ctx, "test.png", data); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(root, "test.png"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, data) {
		t.Fatal("plaintext image persisted")
	}
	if got, err := store.Get(ctx, "test.png"); err != nil || !bytes.Equal(got, data) {
		t.Fatal("encrypted round trip", err)
	}
	if err = store.Put(ctx, "test.png", []byte("overwrite")); !errors.Is(err, ErrExists) {
		t.Fatalf("immutable key overwritten: %v", err)
	}
	if err = local.Put(ctx, "renamed.png", stored); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(ctx, "renamed.png"); !errors.Is(err, ErrIntegrity) {
		t.Fatal("key-scoped authentication bypass")
	}
	stored[len(stored)-4] ^= 1
	if err = os.WriteFile(filepath.Join(root, "test.png"), stored, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(ctx, "test.png"); !errors.Is(err, ErrIntegrity) {
		t.Fatal("tampering accepted")
	}
	for _, key := range []string{"../escape.png", "..\\escape.png", "C:escape.png", "bad\x00.png"} {
		if _, err = store.Get(ctx, key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unsafe key %q", key)
		}
	}
}

func TestLocalGetCannotFollowSymlinkOutsideImageRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("outside-root-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := store.Get(context.Background(), "outside.png"); err == nil {
		t.Fatalf("read through outside-root symlink: %q", data)
	}
}

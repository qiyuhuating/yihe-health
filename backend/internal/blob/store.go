// Package blob stores immutable private images independently of application hosts.
package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"yihe-care/internal/securestore"
)

const MaxPlainBytes = 5 << 20
const MaxStoredBytes = 8 << 20

var ErrNotFound = errors.New("image not found")
var ErrIntegrity = errors.New("image integrity check failed")
var ErrExists = errors.New("immutable image already exists")

type Entry struct {
	Key      string
	Size     int64
	Modified time.Time
}
type Store interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
	Walk(context.Context, func(Entry) error) error
}

func ValidKey(key string) bool {
	if len(key) == 0 || len(key) > 255 || key == "." || key == ".." || !utf8.ValidString(key) || strings.ContainsAny(key, "/\\:") {
		return false
	}
	for _, r := range key {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	if root == "" {
		return nil, errors.New("image directory is required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	return &Local{root: absolute}, nil
}
func (s *Local) path(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrNotFound
	}
	return filepath.Join(s.root, key), nil
}
func (s *Local) Put(ctx context.Context, key string, data []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(data) > MaxStoredBytes {
		return ErrIntegrity
	}
	name, err := s.path(key)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Hard-link publication is atomic and refuses an existing immutable key.
	if err = os.Link(f.Name(), name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrExists
		}
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if dir, err := os.Open(s.root); err == nil {
		defer dir.Close()
		_ = dir.Sync()
	}
	return nil
}
func (s *Local) Get(ctx context.Context, key string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !ValidKey(key) {
		return nil, ErrNotFound
	}
	// Rooted open resolves symlinks inside the trusted directory in one operation.
	// A symlink swapped after validation cannot redirect reads outside the root.
	f, err := os.OpenInRoot(s.root, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxStoredBytes {
		return nil, ErrIntegrity
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxStoredBytes+1))
	if len(data) > MaxStoredBytes {
		return nil, ErrIntegrity
	}
	return data, err
}
func (s *Local) Delete(ctx context.Context, key string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	name, err := s.path(key)
	if err != nil {
		return err
	}
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrIntegrity
	}
	return os.Remove(name)
}
func (s *Local) Walk(ctx context.Context, visit func(Entry) error) error {
	dir, err := os.Open(s.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(256)
		for _, entry := range entries {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".upload-") {
				continue
			}
			info, e := entry.Info()
			if e != nil {
				return e
			}
			if e = visit(Entry{entry.Name(), info.Size(), info.ModTime()}); e != nil {
				return e
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type Encrypted struct {
	Store       Store
	Box         *securestore.Box
	AllowLegacy bool
}

func (s *Encrypted) Put(ctx context.Context, key string, data []byte) error {
	if len(data) > MaxPlainBytes {
		return ErrIntegrity
	}
	sealed, err := s.Box.SealEncodedBytes(data, "image:"+key)
	if err != nil {
		return err
	}
	return s.Store.Put(ctx, key, sealed)
}
func (s *Encrypted) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := s.Store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return s.decode(key, data)
}

func (s *Encrypted) decode(key string, data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte(securestore.Prefix)) {
		if s.AllowLegacy && len(data) <= MaxPlainBytes {
			return data, nil
		}
		return nil, ErrIntegrity
	}
	plain, err := s.Box.OpenEncodedBytes(data, "image:"+key)
	if err != nil {
		return nil, ErrIntegrity
	}
	if len(plain) > MaxPlainBytes {
		return nil, ErrIntegrity
	}
	return plain, nil
}
func (s *Encrypted) Delete(ctx context.Context, key string) error { return s.Store.Delete(ctx, key) }
func (s *Encrypted) Walk(ctx context.Context, visit func(Entry) error) error {
	return s.Store.Walk(ctx, visit)
}

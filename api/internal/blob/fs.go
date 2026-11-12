package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FS stores blobs under a root directory:
//
//	<root>/original/ab/abcdef…   originals (read-only files)
//	<root>/generated/ab/abcdef…  generated images
//	<root>/derived/ab/abcdef…/w512.jpg
//	<root>/tmp/                  in-flight writes
type FS struct {
	root string
}

// NewFS creates the directory layout under root.
func NewFS(root string) (*FS, error) {
	for _, d := range []string{"original", "generated", "derived", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
			return nil, err
		}
	}
	return &FS{root: root}, nil
}

func (s *FS) path(ns Namespace, hash string) string {
	return filepath.Join(s.root, string(ns), hash[:2], hash)
}

func (s *FS) derivedDir(hash string) string {
	return filepath.Join(s.root, "derived", hash[:2], hash)
}

// Put streams r to a temporary file while hashing, then moves it into place.
// If the object already exists the new copy is discarded: content-addressed
// objects are identical by definition, and the existing file is never touched.
func (s *FS) Put(_ context.Context, ns Namespace, r io.Reader) (string, int64, error) {
	if err := checkNS(ns); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "put-*")
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	hash := hex.EncodeToString(h.Sum(nil))

	dst := s.path(ns, hash)
	if _, err := os.Stat(dst); err == nil {
		return hash, size, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return "", 0, err
	}
	if err := os.Chmod(tmp.Name(), 0o400); err != nil {
		return "", 0, err
	}
	// Link instead of rename: link fails if dst appeared meanwhile, so an
	// existing object can never be replaced.
	if err := os.Link(tmp.Name(), dst); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", 0, err
	}
	return hash, size, nil
}

// Open opens an object for reading.
func (s *FS) Open(_ context.Context, ns Namespace, hash string) (Object, error) {
	if err := checkNS(ns); err != nil {
		return nil, err
	}
	if !ValidHash(hash) {
		return nil, ErrNotFound
	}
	return openFile(s.path(ns, hash))
}

// Delete removes an object. Missing objects are not an error.
func (s *FS) Delete(_ context.Context, ns Namespace, hash string) error {
	if err := checkNS(ns); err != nil {
		return err
	}
	if !ValidHash(hash) {
		return fmt.Errorf("blob: invalid hash %q", hash)
	}
	if err := os.Remove(s.path(ns, hash)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// PutDerived writes (or replaces) a derivative of hash.
func (s *FS) PutDerived(_ context.Context, hash, name string, r io.Reader) error {
	if !ValidHash(hash) || !nameRE.MatchString(name) {
		return fmt.Errorf("blob: invalid derivative %q/%q", hash, name)
	}
	dir := s.derivedDir(hash)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "derived-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// OpenDerived opens a derivative.
func (s *FS) OpenDerived(_ context.Context, hash, name string) (Object, error) {
	if !ValidHash(hash) || !nameRE.MatchString(name) {
		return nil, ErrNotFound
	}
	return openFile(filepath.Join(s.derivedDir(hash), name))
}

// DeleteDerived removes all derivatives of hash.
func (s *FS) DeleteDerived(_ context.Context, hash string) error {
	if !ValidHash(hash) {
		return fmt.Errorf("blob: invalid hash %q", hash)
	}
	return os.RemoveAll(s.derivedDir(hash))
}

type file struct {
	*os.File
	size int64
}

func (f file) Size() int64 { return f.size }

func openFile(p string) (Object, error) {
	f, err := os.Open(p) //nolint:gosec // p is built from a validated hash and name
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return file{File: f, size: st.Size()}, nil
}

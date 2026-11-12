// Package blob stores image bytes, content-addressed by SHA-256.
//
// Originals and generated images live in separate namespaces, and derivatives
// (thumbnails) in a third area keyed by the source hash, so a derivative or a
// generated image can never overwrite an original. Objects are write-once:
// putting bytes that already exist is a no-op, and there is no update.
package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// Namespace separates originals from generated content.
type Namespace string

// Namespaces.
const (
	Original  Namespace = "original"
	Generated Namespace = "generated"
)

// ErrNotFound is returned when an object does not exist.
var ErrNotFound = errors.New("blob not found")

// Object is an open blob.
type Object interface {
	io.ReadSeekCloser
	Size() int64
}

// Store is the storage port. The local filesystem implementation is in fs.go;
// an S3-compatible one can be added behind the same interface.
type Store interface {
	// Put stores r and returns its hex SHA-256 and size.
	Put(ctx context.Context, ns Namespace, r io.Reader) (hash string, size int64, err error)
	Open(ctx context.Context, ns Namespace, hash string) (Object, error)
	Delete(ctx context.Context, ns Namespace, hash string) error

	// Derivatives are named files derived from a source hash (e.g. "w512.jpg").
	PutDerived(ctx context.Context, hash, name string, r io.Reader) error
	OpenDerived(ctx context.Context, hash, name string) (Object, error)
	// DeleteDerived removes every derivative of hash.
	DeleteDerived(ctx context.Context, hash string) error
}

var (
	hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// ValidHash reports whether s is a lower-case hex SHA-256.
func ValidHash(s string) bool { return hashRE.MatchString(s) }

// Hash returns the hex SHA-256 of b.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func checkNS(ns Namespace) error {
	if ns != Original && ns != Generated {
		return fmt.Errorf("blob: unknown namespace %q", ns)
	}
	return nil
}

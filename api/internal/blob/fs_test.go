package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func newStore(t *testing.T) *FS {
	t.Helper()
	s, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func read(t *testing.T, o Object) []byte {
	t.Helper()
	defer func() { _ = o.Close() }()
	b, err := io.ReadAll(o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPutOpenRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	data := []byte("original photograph bytes")

	hash, size, err := s.Put(ctx, Original, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if hash != Hash(data) || size != int64(len(data)) {
		t.Fatalf("hash %s size %d", hash, size)
	}
	o, err := s.Open(ctx, Original, hash)
	if err != nil {
		t.Fatal(err)
	}
	if o.Size() != size || !bytes.Equal(read(t, o), data) {
		t.Fatal("bytes differ")
	}

	st, err := os.Stat(s.path(Original, hash))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o222 != 0 {
		t.Fatalf("original is writable: %v", st.Mode())
	}
}

func TestPutIsIdempotentAndNeverReplaces(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	data := []byte("same bytes")

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, _, err := s.Put(ctx, Original, bytes.NewReader(data)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	hash := Hash(data)
	before, _ := os.Stat(s.path(Original, hash))
	if _, _, err := s.Put(ctx, Original, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(s.path(Original, hash))
	if !os.SameFile(before, after) {
		t.Fatal("existing object was replaced")
	}

	entries, _ := os.ReadDir(s.root + "/tmp")
	if len(entries) != 0 {
		t.Fatalf("temporary files left behind: %d", len(entries))
	}
}

func TestNamespacesAreSeparate(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	hash, _, _ := s.Put(ctx, Generated, strings.NewReader("generated"))
	if _, err := s.Open(ctx, Original, hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("generated blob visible as original: %v", err)
	}
	if _, _, err := s.Put(ctx, Namespace("../x"), strings.NewReader("x")); err == nil {
		t.Fatal("unknown namespace accepted")
	}
}

func TestDerived(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	hash, _, _ := s.Put(ctx, Original, strings.NewReader("orig"))

	if err := s.PutDerived(ctx, hash, "w512.jpg", strings.NewReader("thumb")); err != nil {
		t.Fatal(err)
	}
	o, err := s.OpenDerived(ctx, hash, "w512.jpg")
	if err != nil || string(read(t, o)) != "thumb" {
		t.Fatalf("derived: %v", err)
	}
	// The original is untouched.
	orig, _ := s.Open(ctx, Original, hash)
	if string(read(t, orig)) != "orig" {
		t.Fatal("original changed")
	}

	for _, bad := range []string{"../escape", "", "UPPER", "a/b"} {
		if err := s.PutDerived(ctx, hash, bad, strings.NewReader("x")); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}

	if err := s.DeleteDerived(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenDerived(ctx, hash, "w512.jpg"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("derived survived delete: %v", err)
	}
}

func TestOpenRejectsBadHashes(t *testing.T) {
	s := newStore(t)
	for _, h := range []string{"", "../../etc/passwd", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if _, err := s.Open(context.Background(), Original, h); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", h, err)
		}
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	hash, _, _ := s.Put(ctx, Original, strings.NewReader("bye"))
	if err := s.Delete(ctx, Original, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, Original, hash); !errors.Is(err, ErrNotFound) {
		t.Fatal("still there")
	}
	if err := s.Delete(ctx, Original, hash); err != nil {
		t.Fatalf("deleting a missing blob: %v", err)
	}
}

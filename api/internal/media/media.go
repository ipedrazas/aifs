// Package media serves registered blobs and their thumbnails.
package media

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/imaging"
)

// ErrNotFound is returned for an unknown blob.
var ErrNotFound = errors.New("not found")

// ThumbnailWidths are the widths thumbnails are rendered at. Requested
// widths round up to the next one, which bounds the number of derivatives.
var ThumbnailWidths = []int{256, 512, 1024, 2048}

// Meta describes a registered blob.
type Meta struct {
	Hash        string
	Namespace   blob.Namespace
	ContentType string
	Orientation int
	CreatedAt   time.Time
}

// Service reads blobs registered in the database.
type Service struct {
	db    *db.DB
	store blob.Store
	// render limits concurrent thumbnail renders: each decode can hold a
	// full-resolution bitmap in memory.
	render chan struct{}
}

// New returns a media service.
func New(database *db.DB, store blob.Store) *Service {
	return &Service{db: database, store: store, render: make(chan struct{}, 2)}
}

func (s *Service) meta(ctx context.Context, hash string) (Meta, error) {
	if !blob.ValidHash(hash) {
		return Meta{}, ErrNotFound
	}
	m := Meta{Hash: hash}
	var ns, created string
	err := s.db.Read.QueryRowContext(ctx,
		`SELECT namespace, content_type, orientation, created_at FROM blob WHERE hash = ?`, hash).
		Scan(&ns, &m.ContentType, &m.Orientation, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Meta{}, ErrNotFound
	}
	if err != nil {
		return Meta{}, err
	}
	m.Namespace = blob.Namespace(ns)
	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return m, nil
}

// Open returns the original bytes of a registered blob.
func (s *Service) Open(ctx context.Context, hash string) (blob.Object, Meta, error) {
	m, err := s.meta(ctx, hash)
	if err != nil {
		return nil, Meta{}, err
	}
	o, err := s.store.Open(ctx, m.Namespace, hash)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, Meta{}, fmt.Errorf("blob %s registered but missing from the store: %w", hash, err)
	}
	return o, m, err
}

// SnapWidth rounds w up to a supported thumbnail width.
func SnapWidth(w int) int {
	for _, tw := range ThumbnailWidths {
		if w <= tw {
			return tw
		}
	}
	return ThumbnailWidths[len(ThumbnailWidths)-1]
}

// Thumbnail returns an upright JPEG of the blob at most width pixels wide,
// rendering and caching it on first use. The original is never modified.
func (s *Service) Thumbnail(ctx context.Context, hash string, width int) (blob.Object, Meta, int, error) {
	m, err := s.meta(ctx, hash)
	if err != nil {
		return nil, Meta{}, 0, err
	}
	width = SnapWidth(width)
	name := fmt.Sprintf("w%d.jpg", width)

	if o, err := s.store.OpenDerived(ctx, hash, name); err == nil {
		return o, m, width, nil
	} else if !errors.Is(err, blob.ErrNotFound) {
		return nil, Meta{}, 0, err
	}

	select {
	case s.render <- struct{}{}:
		defer func() { <-s.render }()
	case <-ctx.Done():
		return nil, Meta{}, 0, ctx.Err()
	}

	src, err := s.store.Open(ctx, m.Namespace, hash)
	if err != nil {
		return nil, Meta{}, 0, err
	}
	var buf bytes.Buffer
	err = imaging.Thumbnail(src, m.Orientation, width, &buf)
	_ = src.Close()
	if err != nil {
		return nil, Meta{}, 0, fmt.Errorf("render thumbnail: %w", err)
	}
	if err := s.store.PutDerived(ctx, hash, name, &buf); err != nil {
		return nil, Meta{}, 0, err
	}
	o, err := s.store.OpenDerived(ctx, hash, name)
	return o, m, width, err
}

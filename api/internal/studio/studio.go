// Package studio runs photoshoots: it plans generations from a recipe,
// executes them through providers in a durable background runner, and
// records every generated image with the full snapshot that produced it.
package studio

import (
	"log/slog"
	"time"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/provider"
)

// ErrNotFound is shared with the catalog so the HTTP layer maps both alike.
var ErrNotFound = catalog.ErrNotFound

type (
	// ValidationError lists invalid fields.
	ValidationError = catalog.ValidationError
	// ConflictError is a request that cannot be applied in the current state.
	ConflictError = catalog.ConflictError
)

func invalid(field, reason string) error {
	return &ValidationError{Fields: map[string]string{field: reason}}
}

// Config tunes generation.
type Config struct {
	// MaxVariations caps the variations in one generation.
	MaxVariations int
}

// Service is the photoshoot and generation API.
type Service struct {
	db        *db.DB
	blobs     blob.Store
	catalog   *catalog.Service
	providers *provider.Registry
	runner    *Runner
	log       *slog.Logger
	cfg       Config
	now       func() time.Time
}

// New returns a studio service. The runner executes generations; start it
// with Runner.Run.
func New(database *db.DB, blobs blob.Store, cat *catalog.Service, providers *provider.Registry, runner *Runner, log *slog.Logger, cfg Config) *Service {
	if cfg.MaxVariations <= 0 {
		cfg.MaxVariations = 4
	}
	return &Service{db: database, blobs: blobs, catalog: cat, providers: providers, runner: runner, log: log, cfg: cfg, now: time.Now}
}

func (s *Service) timestamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func parseTimePtr(v *string) *time.Time {
	if v == nil || *v == "" {
		return nil
	}
	t := parseTime(*v)
	return &t
}

// Package catalog manages products, virtual models and their reference
// photographs.
package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/imaging"
)

var (
	// ErrNotFound is returned for a missing product, model or image.
	ErrNotFound = errors.New("not found")
)

// ConflictError is returned when a write would violate a uniqueness rule.
type ConflictError struct{ Reason string }

func (e *ConflictError) Error() string { return e.Reason }

// ValidationError lists invalid fields.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for k, v := range e.Fields {
		parts = append(parts, k+": "+v)
	}
	return "invalid input: " + strings.Join(parts, "; ")
}

// Service implements the catalog on top of SQLite and a blob store.
type Service struct {
	db    *db.DB
	blobs blob.Store
	log   *slog.Logger
	now   func() time.Time
	// blobMu serialises "store bytes + register blob" against "unregister
	// blob + delete bytes" so a concurrent upload and delete of identical
	// bytes cannot leave a row without a file.
	blobMu sync.Mutex
}

// New returns a catalog service.
func New(database *db.DB, blobs blob.Store, log *slog.Logger) *Service {
	return &Service{db: database, blobs: blobs, log: log, now: time.Now}
}

func (s *Service) timestamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

const (
	maxName  = 200
	maxSKU   = 64
	maxShort = 100
	maxText  = 5000
)

type field struct {
	name  string
	value *string
	max   int
}

// normaliseFields trims every set field and checks its length. The name
// field is required on create and may not be cleared on update.
func normaliseFields(fields []field, name *string, create bool) error {
	errs := map[string]string{}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		v := strings.TrimSpace(*f.value)
		*f.value = v
		if !utf8.ValidString(v) {
			errs[f.name] = "must be valid text"
		} else if utf8.RuneCountInString(v) > f.max {
			errs[f.name] = fmt.Sprintf("must be at most %d characters", f.max)
		}
	}
	if (create && (name == nil || *name == "")) || (!create && name != nil && *name == "") {
		errs["name"] = "is required"
	}
	if len(errs) > 0 {
		return &ValidationError{Fields: errs}
	}
	return nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullable(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

func sqliteCode(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

func isUnique(err error) bool { return sqliteCode(err) == sqlite3.SQLITE_CONSTRAINT_UNIQUE }

// isReferenced reports a foreign-key violation: the row is still referenced.
// SQLite reports ON DELETE RESTRICT as SQLITE_CONSTRAINT_TRIGGER and deferred
// or plain checks as SQLITE_CONSTRAINT_FOREIGNKEY, so match the family and
// the message.
func isReferenced(err error) bool {
	return err != nil && sqliteCode(err)&0xff == sqlite3.SQLITE_CONSTRAINT &&
		strings.Contains(err.Error(), "FOREIGN KEY")
}

type scanner interface{ Scan(...any) error }

// storeOriginal writes validated photograph bytes to the blob store and
// registers them in tx. Must be called with blobMu held, and the transaction
// committed before blobMu is released.
func (s *Service) storeOriginal(ctx context.Context, tx *sql.Tx, data []byte, info imaging.Info, now string) (string, error) {
	hash, size, err := s.blobs.Put(ctx, blob.Original, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("store photograph: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO blob (hash, namespace, content_type, size, width, height, orientation, created_at)
		VALUES (?, 'original', ?, ?, ?, ?, ?, ?) ON CONFLICT (hash) DO NOTHING`,
		hash, info.ContentType, size, info.Width, info.Height, info.Orientation, now); err != nil {
		return "", err
	}
	return hash, nil
}

func (s *Service) imageHashes(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.Read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// collect removes blobs nothing refers to any more. The foreign keys decide:
// deleting a blob row that is still referenced fails with a constraint error
// and the blob is kept. Must be called with blobMu held.
func (s *Service) collect(ctx context.Context, hashes []string) {
	for _, h := range hashes {
		res, err := s.db.Write.ExecContext(ctx, `DELETE FROM blob WHERE hash = ? AND namespace = 'original'`, h)
		if isReferenced(err) {
			continue // still referenced
		}
		if err != nil {
			s.log.ErrorContext(ctx, "blob gc: delete row", "hash", h, "err", err)
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if err := s.blobs.Delete(ctx, blob.Original, h); err != nil {
			s.log.ErrorContext(ctx, "blob gc: delete bytes", "hash", h, "err", err)
		}
		if err := s.blobs.DeleteDerived(ctx, h); err != nil {
			s.log.ErrorContext(ctx, "blob gc: delete derivatives", "hash", h, "err", err)
		}
	}
}

// cleanFilename keeps the base name of the uploaded file for display only.
func cleanFilename(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	return name
}

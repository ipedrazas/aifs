// Package catalog manages products and their reference photographs.
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
	// ErrNotFound is returned for a missing product or image.
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

// Views a reference photograph can be labelled with.
var Views = []string{"front", "back", "side", "detail", "print"}

// DefaultCategory is used when a product has none.
const DefaultCategory = "jeans"

// Product is a garment with its reference photographs.
type Product struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	SKU          string    `json:"sku"`
	Category     string    `json:"category"`
	Description  string    `json:"description"`
	Colour       string    `json:"colour"`
	PrintDetails string    `json:"printDetails"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	ImageCount   int       `json:"imageCount"`
	// Cover is the first front photograph, else the first photograph.
	Cover *Image `json:"cover"`
	// Images is only filled by Get.
	Images []Image `json:"images,omitempty"`
}

// Image is a reference photograph.
type Image struct {
	ID          int64     `json:"id"`
	ProductID   int64     `json:"productId"`
	Hash        string    `json:"hash"`
	View        string    `json:"view"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Input holds every editable product field. On update, nil fields are left
// unchanged.
type Input struct {
	Name         *string `json:"name"`
	SKU          *string `json:"sku"`
	Category     *string `json:"category"`
	Description  *string `json:"description"`
	Colour       *string `json:"colour"`
	PrintDetails *string `json:"printDetails"`
	Notes        *string `json:"notes"`
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

func (in *Input) fields() []field {
	return []field{
		{"name", in.Name, maxName},
		{"sku", in.SKU, maxSKU},
		{"category", in.Category, maxShort},
		{"description", in.Description, maxText},
		{"colour", in.Colour, maxText},
		{"printDetails", in.PrintDetails, maxText},
		{"notes", in.Notes, maxText},
	}
}

// normalise trims fields and checks lengths. create requires a name.
func (in *Input) normalise(create bool) error {
	errs := map[string]string{}
	for _, f := range in.fields() {
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
	if (create && (in.Name == nil || *in.Name == "")) || (!create && in.Name != nil && *in.Name == "") {
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

// Create adds a product.
func (s *Service) Create(ctx context.Context, in Input) (Product, error) {
	if err := in.normalise(true); err != nil {
		return Product{}, err
	}
	category := str(in.Category)
	if category == "" {
		category = DefaultCategory
	}
	now := s.timestamp()
	res, err := s.db.Write.ExecContext(ctx, `
		INSERT INTO product (name, sku, category, description, colour, print_details, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		*in.Name, nullable(in.SKU), category, str(in.Description), str(in.Colour), str(in.PrintDetails), str(in.Notes), now, now)
	if isUnique(err) {
		return Product{}, &ConflictError{Reason: "another product already uses this identifier"}
	}
	if err != nil {
		return Product{}, err
	}
	id, _ := res.LastInsertId()
	return s.Get(ctx, id)
}

// Update changes the fields set in in.
func (s *Service) Update(ctx context.Context, id int64, in Input) (Product, error) {
	if err := in.normalise(false); err != nil {
		return Product{}, err
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) { sets = append(sets, col+" = ?"); args = append(args, v) }
	if in.Name != nil {
		add("name", *in.Name)
	}
	if in.SKU != nil {
		add("sku", nullable(in.SKU))
	}
	if in.Category != nil {
		c := *in.Category
		if c == "" {
			c = DefaultCategory
		}
		add("category", c)
	}
	if in.Description != nil {
		add("description", *in.Description)
	}
	if in.Colour != nil {
		add("colour", *in.Colour)
	}
	if in.PrintDetails != nil {
		add("print_details", *in.PrintDetails)
	}
	if in.Notes != nil {
		add("notes", *in.Notes)
	}
	add("updated_at", s.timestamp())
	args = append(args, id)

	res, err := s.db.Write.ExecContext(ctx, `UPDATE product SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...) //nolint:gosec // column names are constants
	if isUnique(err) {
		return Product{}, &ConflictError{Reason: "another product already uses this identifier"}
	}
	if err != nil {
		return Product{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Product{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

const productCols = `p.id, p.name, COALESCE(p.sku, ''), p.category, p.description, p.colour, p.print_details, p.notes,
	p.created_at, p.updated_at, (SELECT count(*) FROM product_image WHERE product_id = p.id)`

const imageCols = `i.id, i.product_id, i.blob_hash, i.view, i.filename, b.content_type, b.size, b.width, b.height, i.created_at`

// viewOrder sorts photographs front, back, side, detail, print.
const viewOrder = `CASE view WHEN 'front' THEN 0 WHEN 'back' THEN 1 WHEN 'side' THEN 2 WHEN 'detail' THEN 3 ELSE 4 END`

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (Product, error) {
	var p Product
	var created, updated string
	err := row.Scan(&p.ID, &p.Name, &p.SKU, &p.Category, &p.Description, &p.Colour, &p.PrintDetails, &p.Notes,
		&created, &updated, &p.ImageCount)
	p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
	return p, err
}

func scanImage(row scanner) (Image, error) {
	var im Image
	var created string
	err := row.Scan(&im.ID, &im.ProductID, &im.Hash, &im.View, &im.Filename, &im.ContentType, &im.Size, &im.Width, &im.Height, &created)
	im.CreatedAt = parseTime(created)
	return im, err
}

// List returns all products, newest first, each with its cover photograph.
func (s *Service) List(ctx context.Context) ([]Product, error) {
	rows, err := s.db.Read.QueryContext(ctx, `SELECT `+productCols+` FROM product p ORDER BY p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	covers, err := s.covers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if c, ok := covers[out[i].ID]; ok {
			out[i].Cover = &c
		}
	}
	return out, nil
}

func (s *Service) covers(ctx context.Context) (map[int64]Image, error) {
	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT `+imageCols+` FROM product_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.id = (
			SELECT id FROM product_image WHERE product_id = i.product_id
			ORDER BY `+viewOrder+`, id LIMIT 1
		)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]Image{}
	for rows.Next() {
		im, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		out[im.ProductID] = im
	}
	return out, rows.Err()
}

// Get returns a product with all its photographs.
func (s *Service) Get(ctx context.Context, id int64) (Product, error) {
	p, err := scanProduct(s.db.Read.QueryRowContext(ctx, `SELECT `+productCols+` FROM product p WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}

	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT `+imageCols+` FROM product_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.product_id = ?
		ORDER BY `+viewOrder+`, i.id`, id)
	if err != nil {
		return Product{}, err
	}
	defer func() { _ = rows.Close() }()
	p.Images = []Image{}
	for rows.Next() {
		im, err := scanImage(rows)
		if err != nil {
			return Product{}, err
		}
		p.Images = append(p.Images, im)
	}
	if err := rows.Err(); err != nil {
		return Product{}, err
	}
	// Images are ordered front first, so the first one is the cover.
	if len(p.Images) > 0 {
		c := p.Images[0]
		p.Cover = &c
	}
	return p, nil
}

// Delete removes a product, its image records, and any photograph bytes no
// longer referenced.
func (s *Service) Delete(ctx context.Context, id int64) error {
	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	hashes, err := s.imageHashes(ctx, `SELECT blob_hash FROM product_image WHERE product_id = ?`, id)
	if err != nil {
		return err
	}
	res, err := s.db.Write.ExecContext(ctx, `DELETE FROM product WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.collect(ctx, hashes)
	return nil
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
		if sqliteCode(err) == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
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

// AddImage validates and stores a reference photograph. The bytes are stored
// exactly as uploaded.
func (s *Service) AddImage(ctx context.Context, productID int64, view, filename string, data []byte) (Image, error) {
	if !validView(view) {
		return Image{}, &ValidationError{Fields: map[string]string{"view": "must be one of " + strings.Join(Views, ", ")}}
	}
	info, err := imaging.Validate(data)
	if err != nil {
		return Image{}, err
	}
	filename = cleanFilename(filename)

	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	var exists int
	if err := s.db.Read.QueryRowContext(ctx, `SELECT count(*) FROM product WHERE id = ?`, productID).Scan(&exists); err != nil {
		return Image{}, err
	}
	if exists == 0 {
		return Image{}, ErrNotFound
	}

	hash, size, err := s.blobs.Put(ctx, blob.Original, bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("store photograph: %w", err)
	}

	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return Image{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := s.timestamp()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO blob (hash, namespace, content_type, size, width, height, orientation, created_at)
		VALUES (?, 'original', ?, ?, ?, ?, ?, ?) ON CONFLICT (hash) DO NOTHING`,
		hash, info.ContentType, size, info.Width, info.Height, info.Orientation, now); err != nil {
		return Image{}, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO product_image (product_id, blob_hash, view, filename, created_at) VALUES (?, ?, ?, ?, ?)`,
		productID, hash, view, filename, now)
	if isUnique(err) {
		return Image{}, &ConflictError{Reason: "this photograph is already attached to the product"}
	}
	if err != nil {
		return Image{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE product SET updated_at = ? WHERE id = ?`, now, productID); err != nil {
		return Image{}, err
	}
	if err := tx.Commit(); err != nil {
		return Image{}, err
	}
	id, _ := res.LastInsertId()
	return s.image(ctx, productID, id)
}

func (s *Service) image(ctx context.Context, productID, imageID int64) (Image, error) {
	im, err := scanImage(s.db.Read.QueryRowContext(ctx, `
		SELECT `+imageCols+` FROM product_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.id = ? AND i.product_id = ?`, imageID, productID))
	if errors.Is(err, sql.ErrNoRows) {
		return Image{}, ErrNotFound
	}
	return im, err
}

// DeleteImage removes one reference photograph record (and its bytes, if no
// other record uses them).
func (s *Service) DeleteImage(ctx context.Context, productID, imageID int64) error {
	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	im, err := s.image(ctx, productID, imageID)
	if err != nil {
		return err
	}
	if _, err := s.db.Write.ExecContext(ctx, `DELETE FROM product_image WHERE id = ?`, imageID); err != nil {
		return err
	}
	if _, err := s.db.Write.ExecContext(ctx, `UPDATE product SET updated_at = ? WHERE id = ?`, s.timestamp(), productID); err != nil {
		return err
	}
	s.collect(ctx, []string{im.Hash})
	return nil
}

func validView(v string) bool {
	for _, x := range Views {
		if v == x {
			return true
		}
	}
	return false
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

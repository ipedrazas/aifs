package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ipedrazas/aifs/api/internal/imaging"
)

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
	Cover *ProductImage `json:"cover"`
	// Images is only filled by Get.
	Images []ProductImage `json:"images,omitempty"`
}

// ProductImage is a reference photograph.
type ProductImage struct {
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

// ProductInput holds every editable product field. On update, nil fields are left
// unchanged.
type ProductInput struct {
	Name         *string `json:"name"`
	SKU          *string `json:"sku"`
	Category     *string `json:"category"`
	Description  *string `json:"description"`
	Colour       *string `json:"colour"`
	PrintDetails *string `json:"printDetails"`
	Notes        *string `json:"notes"`
}

func (in *ProductInput) fields() []field {
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

func (in *ProductInput) normalise(create bool) error {
	return normaliseFields(in.fields(), in.Name, create)
}

// CreateProduct adds a product.
func (s *Service) CreateProduct(ctx context.Context, in ProductInput) (Product, error) {
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
	return s.GetProduct(ctx, id)
}

// UpdateProduct changes the fields set in in.
func (s *Service) UpdateProduct(ctx context.Context, id int64, in ProductInput) (Product, error) {
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
	return s.GetProduct(ctx, id)
}

const productCols = `p.id, p.name, COALESCE(p.sku, ''), p.category, p.description, p.colour, p.print_details, p.notes,
	p.created_at, p.updated_at, (SELECT count(*) FROM product_image WHERE product_id = p.id)`

const productImageCols = `i.id, i.product_id, i.blob_hash, i.view, i.filename, b.content_type, b.size, b.width, b.height, i.created_at`

// viewOrder sorts photographs front, back, side, detail, print.
const viewOrder = `CASE view WHEN 'front' THEN 0 WHEN 'back' THEN 1 WHEN 'side' THEN 2 WHEN 'detail' THEN 3 ELSE 4 END`

func scanProduct(row scanner) (Product, error) {
	var p Product
	var created, updated string
	err := row.Scan(&p.ID, &p.Name, &p.SKU, &p.Category, &p.Description, &p.Colour, &p.PrintDetails, &p.Notes,
		&created, &updated, &p.ImageCount)
	p.CreatedAt, p.UpdatedAt = parseTime(created), parseTime(updated)
	return p, err
}

func scanProductImage(row scanner) (ProductImage, error) {
	var im ProductImage
	var created string
	err := row.Scan(&im.ID, &im.ProductID, &im.Hash, &im.View, &im.Filename, &im.ContentType, &im.Size, &im.Width, &im.Height, &created)
	im.CreatedAt = parseTime(created)
	return im, err
}

// ListProducts returns all products, newest first, each with its cover photograph.
func (s *Service) ListProducts(ctx context.Context) ([]Product, error) {
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

	covers, err := s.productCovers(ctx)
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

func (s *Service) productCovers(ctx context.Context) (map[int64]ProductImage, error) {
	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT `+productImageCols+` FROM product_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.id = (
			SELECT id FROM product_image WHERE product_id = i.product_id
			ORDER BY `+viewOrder+`, id LIMIT 1
		)`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]ProductImage{}
	for rows.Next() {
		im, err := scanProductImage(rows)
		if err != nil {
			return nil, err
		}
		out[im.ProductID] = im
	}
	return out, rows.Err()
}

// GetProduct returns a product with all its photographs.
func (s *Service) GetProduct(ctx context.Context, id int64) (Product, error) {
	p, err := scanProduct(s.db.Read.QueryRowContext(ctx, `SELECT `+productCols+` FROM product p WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}

	rows, err := s.db.Read.QueryContext(ctx, `
		SELECT `+productImageCols+` FROM product_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.product_id = ?
		ORDER BY `+viewOrder+`, i.id`, id)
	if err != nil {
		return Product{}, err
	}
	defer func() { _ = rows.Close() }()
	p.Images = []ProductImage{}
	for rows.Next() {
		im, err := scanProductImage(rows)
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

// DeleteProduct removes a product, its image records, and any photograph bytes no
// longer referenced.
func (s *Service) DeleteProduct(ctx context.Context, id int64) error {
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

// AddProductImage validates and stores a reference photograph. The bytes are stored
// exactly as uploaded.
func (s *Service) AddProductImage(ctx context.Context, productID int64, view, filename string, data []byte) (ProductImage, error) {
	if !validView(view) {
		return ProductImage{}, &ValidationError{Fields: map[string]string{"view": "must be one of " + strings.Join(Views, ", ")}}
	}
	info, err := imaging.Validate(data)
	if err != nil {
		return ProductImage{}, err
	}
	filename = cleanFilename(filename)

	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	var exists int
	if err := s.db.Read.QueryRowContext(ctx, `SELECT count(*) FROM product WHERE id = ?`, productID).Scan(&exists); err != nil {
		return ProductImage{}, err
	}
	if exists == 0 {
		return ProductImage{}, ErrNotFound
	}

	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return ProductImage{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := s.timestamp()
	hash, err := s.storeOriginal(ctx, tx, data, info, now)
	if err != nil {
		return ProductImage{}, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO product_image (product_id, blob_hash, view, filename, created_at) VALUES (?, ?, ?, ?, ?)`,
		productID, hash, view, filename, now)
	if isUnique(err) {
		return ProductImage{}, &ConflictError{Reason: "this photograph is already attached to the product"}
	}
	if err != nil {
		return ProductImage{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE product SET updated_at = ? WHERE id = ?`, now, productID); err != nil {
		return ProductImage{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProductImage{}, err
	}
	id, _ := res.LastInsertId()
	return s.productImage(ctx, productID, id)
}

func (s *Service) productImage(ctx context.Context, productID, imageID int64) (ProductImage, error) {
	im, err := scanProductImage(s.db.Read.QueryRowContext(ctx, `
		SELECT `+productImageCols+` FROM product_image i JOIN blob b ON b.hash = i.blob_hash
		WHERE i.id = ? AND i.product_id = ?`, imageID, productID))
	if errors.Is(err, sql.ErrNoRows) {
		return ProductImage{}, ErrNotFound
	}
	return im, err
}

// DeleteProductImage removes one reference photograph record (and its bytes, if no
// other record uses them).
func (s *Service) DeleteProductImage(ctx context.Context, productID, imageID int64) error {
	s.blobMu.Lock()
	defer s.blobMu.Unlock()

	im, err := s.productImage(ctx, productID, imageID)
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

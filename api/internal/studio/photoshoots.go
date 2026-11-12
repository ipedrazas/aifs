package studio

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ipedrazas/aifs/api/internal/catalog"
)

// AspectRatios a photoshoot can use.
var AspectRatios = []string{"3:4", "4:5", "2:3", "1:1", "9:16", "4:3", "16:9"}

// Photoshoot is a reusable product + model configuration.
type Photoshoot struct {
	ID                 int64          `json:"id"`
	Name               string         `json:"name"`
	ProductID          int64          `json:"productId"`
	ModelID            int64          `json:"modelId"`
	AspectRatio        string         `json:"aspectRatio"`
	CustomInstructions string         `json:"customInstructions"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
	Product            ProductSummary `json:"product"`
	Model              ModelSummary   `json:"model"`
	GenerationCount    int            `json:"generationCount"`
	// LatestImageHash is the newest generated image, for list thumbnails.
	LatestImageHash string `json:"latestImageHash"`
	// Generations is only filled by GetPhotoshoot, newest first.
	Generations []Generation `json:"generations,omitempty"`
}

// ProductSummary identifies a product in other resources.
type ProductSummary struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SKU       string `json:"sku"`
	CoverHash string `json:"coverHash"`
}

// ModelSummary identifies a model in other resources.
type ModelSummary struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Archived  bool   `json:"archived"`
	CoverHash string `json:"coverHash"`
}

// PhotoshootInput holds editable fields; nil fields are unchanged on update.
type PhotoshootInput struct {
	Name               *string `json:"name"`
	ProductID          *int64  `json:"productId"`
	ModelID            *int64  `json:"modelId"`
	AspectRatio        *string `json:"aspectRatio"`
	CustomInstructions *string `json:"customInstructions"`
}

const maxInstructions = 2000

func (in *PhotoshootInput) normalise(create bool) error {
	errs := map[string]string{}
	trim := func(p *string) {
		if p != nil {
			*p = strings.TrimSpace(*p)
		}
	}
	trim(in.Name)
	trim(in.AspectRatio)
	trim(in.CustomInstructions)
	if in.Name != nil && utf8.RuneCountInString(*in.Name) > 200 {
		errs["name"] = "must be at most 200 characters"
	}
	if in.CustomInstructions != nil && utf8.RuneCountInString(*in.CustomInstructions) > maxInstructions {
		errs["customInstructions"] = fmt.Sprintf("must be at most %d characters", maxInstructions)
	}
	if in.AspectRatio != nil && *in.AspectRatio != "" && !contains(AspectRatios, *in.AspectRatio) {
		errs["aspectRatio"] = "must be one of " + strings.Join(AspectRatios, ", ")
	}
	if create && in.ProductID == nil {
		errs["productId"] = "is required"
	}
	if create && in.ModelID == nil {
		errs["modelId"] = "is required"
	}
	if len(errs) > 0 {
		return &ValidationError{Fields: errs}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// CreatePhotoshoot saves a product + model combination. The name defaults to
// "<product> on <model>".
func (s *Service) CreatePhotoshoot(ctx context.Context, in PhotoshootInput) (Photoshoot, error) {
	if err := in.normalise(true); err != nil {
		return Photoshoot{}, err
	}
	prod, model, err := s.checkRefs(ctx, *in.ProductID, *in.ModelID)
	if err != nil {
		return Photoshoot{}, err
	}
	name := ""
	if in.Name != nil {
		name = *in.Name
	}
	if name == "" {
		name = truncate(prod.Name+" on "+model.Name, 200)
	}
	ratio := "3:4"
	if in.AspectRatio != nil && *in.AspectRatio != "" {
		ratio = *in.AspectRatio
	}
	instructions := ""
	if in.CustomInstructions != nil {
		instructions = *in.CustomInstructions
	}

	now := s.timestamp()
	res, err := s.db.Write.ExecContext(ctx, `
		INSERT INTO photoshoot (name, product_id, model_id, aspect_ratio, custom_instructions, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, name, prod.ID, model.ID, ratio, instructions, now, now)
	if err != nil {
		return Photoshoot{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetPhotoshoot(ctx, id)
}

// checkRefs loads the product and model a photoshoot points at. Archived
// models cannot be chosen.
func (s *Service) checkRefs(ctx context.Context, productID, modelID int64) (catalog.Product, catalog.Model, error) {
	prod, err := s.catalog.GetProduct(ctx, productID)
	if errors.Is(err, catalog.ErrNotFound) {
		return catalog.Product{}, catalog.Model{}, invalid("productId", "no such product")
	}
	if err != nil {
		return catalog.Product{}, catalog.Model{}, err
	}
	model, err := s.catalog.GetModel(ctx, modelID)
	if errors.Is(err, catalog.ErrNotFound) {
		return catalog.Product{}, catalog.Model{}, invalid("modelId", "no such model")
	}
	if err != nil {
		return catalog.Product{}, catalog.Model{}, err
	}
	if model.Archived {
		return catalog.Product{}, catalog.Model{}, invalid("modelId", "this model is archived; restore it first")
	}
	return prod, model, nil
}

// UpdatePhotoshoot changes the fields set in in. Past generations keep the
// snapshot they were made with.
func (s *Service) UpdatePhotoshoot(ctx context.Context, id int64, in PhotoshootInput) (Photoshoot, error) {
	if err := in.normalise(false); err != nil {
		return Photoshoot{}, err
	}
	cur, err := s.GetPhotoshoot(ctx, id)
	if err != nil {
		return Photoshoot{}, err
	}
	productID, modelID := cur.ProductID, cur.ModelID
	if in.ProductID != nil {
		productID = *in.ProductID
	}
	if in.ModelID != nil {
		modelID = *in.ModelID
	}
	if in.ProductID != nil || in.ModelID != nil {
		if _, _, err := s.checkRefs(ctx, productID, modelID); err != nil {
			return Photoshoot{}, err
		}
	}

	sets := []string{"product_id = ?", "model_id = ?"}
	args := []any{productID, modelID}
	if in.Name != nil {
		if *in.Name == "" {
			return Photoshoot{}, invalid("name", "is required")
		}
		sets, args = append(sets, "name = ?"), append(args, *in.Name)
	}
	if in.AspectRatio != nil && *in.AspectRatio != "" {
		sets, args = append(sets, "aspect_ratio = ?"), append(args, *in.AspectRatio)
	}
	if in.CustomInstructions != nil {
		sets, args = append(sets, "custom_instructions = ?"), append(args, *in.CustomInstructions)
	}
	sets, args = append(sets, "updated_at = ?"), append(args, s.timestamp())
	args = append(args, id)
	if _, err := s.db.Write.ExecContext(ctx, `UPDATE photoshoot SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil { //nolint:gosec // column names are constants
		return Photoshoot{}, err
	}
	return s.GetPhotoshoot(ctx, id)
}

// DeletePhotoshoot removes a photoshoot that has never been generated from.
func (s *Service) DeletePhotoshoot(ctx context.Context, id int64) error {
	res, err := s.db.Write.ExecContext(ctx, `DELETE FROM photoshoot WHERE id = ?`, id)
	if err != nil && strings.Contains(err.Error(), "FOREIGN KEY") {
		return &ConflictError{Reason: "this photoshoot has generations and is kept for traceability"}
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const photoshootCols = `ps.id, ps.name, ps.product_id, ps.model_id, ps.aspect_ratio, ps.custom_instructions,
	ps.created_at, ps.updated_at,
	p.name, COALESCE(p.sku, ''),
	COALESCE((SELECT pi.blob_hash FROM product_image pi WHERE pi.product_id = p.id
		ORDER BY CASE pi.view WHEN 'front' THEN 0 WHEN 'back' THEN 1 WHEN 'side' THEN 2 WHEN 'detail' THEN 3 ELSE 4 END, pi.id LIMIT 1), ''),
	m.name, m.archived_at IS NOT NULL,
	COALESCE((SELECT mi.blob_hash FROM model_image mi WHERE mi.model_id = m.id
		ORDER BY CASE mi.role WHEN 'identity' THEN 0 ELSE 1 END, mi.id LIMIT 1), ''),
	(SELECT count(*) FROM generation g WHERE g.photoshoot_id = ps.id),
	COALESCE((SELECT gi.blob_hash FROM generated_image gi JOIN generation g ON g.id = gi.generation_id
		WHERE g.photoshoot_id = ps.id ORDER BY gi.id DESC LIMIT 1), '')`

const photoshootFrom = ` FROM photoshoot ps JOIN product p ON p.id = ps.product_id JOIN model m ON m.id = ps.model_id`

type scanner interface{ Scan(...any) error }

func scanPhotoshoot(row scanner) (Photoshoot, error) {
	var ps Photoshoot
	var created, updated string
	err := row.Scan(&ps.ID, &ps.Name, &ps.ProductID, &ps.ModelID, &ps.AspectRatio, &ps.CustomInstructions,
		&created, &updated,
		&ps.Product.Name, &ps.Product.SKU, &ps.Product.CoverHash,
		&ps.Model.Name, &ps.Model.Archived, &ps.Model.CoverHash,
		&ps.GenerationCount, &ps.LatestImageHash)
	ps.CreatedAt, ps.UpdatedAt = parseTime(created), parseTime(updated)
	ps.Product.ID, ps.Model.ID = ps.ProductID, ps.ModelID
	return ps, err
}

// ListPhotoshoots returns photoshoots, most recently updated first,
// optionally only those for one product.
func (s *Service) ListPhotoshoots(ctx context.Context, productID int64) ([]Photoshoot, error) {
	q := `SELECT ` + photoshootCols + photoshootFrom
	var args []any
	if productID > 0 {
		q += ` WHERE ps.product_id = ?`
		args = append(args, productID)
	}
	rows, err := s.db.Read.QueryContext(ctx, q+` ORDER BY ps.updated_at DESC, ps.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Photoshoot{}
	for rows.Next() {
		ps, err := scanPhotoshoot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// GetPhotoshoot returns a photoshoot with its generations, newest first.
func (s *Service) GetPhotoshoot(ctx context.Context, id int64) (Photoshoot, error) {
	ps, err := scanPhotoshoot(s.db.Read.QueryRowContext(ctx, `SELECT `+photoshootCols+photoshootFrom+` WHERE ps.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Photoshoot{}, ErrNotFound
	}
	if err != nil {
		return Photoshoot{}, err
	}
	ps.Generations, err = s.listGenerations(ctx, `WHERE g.photoshoot_id = ?`, id)
	return ps, err
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

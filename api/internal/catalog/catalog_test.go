package catalog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/imaging"
)

func setup(t *testing.T) (*Service, *db.DB, *blob.FS) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	store, err := blob.NewFS(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	return New(d, store, slog.New(slog.NewTextHandler(io.Discard, nil))), d, store
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "imaging", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func p(s string) *string { return &s }

func TestCreateGetUpdate(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)

	prod, err := s.CreateProduct(ctx, ProductInput{Name: p("  Slim jeans "), SKU: p("SJ-001"), Colour: p("indigo"), PrintDetails: p("embroidered back pocket")})
	if err != nil {
		t.Fatal(err)
	}
	if prod.Name != "Slim jeans" || prod.SKU != "SJ-001" || prod.Category != DefaultCategory || prod.ImageCount != 0 || prod.Images == nil {
		t.Fatalf("created: %+v", prod)
	}

	up, err := s.UpdateProduct(ctx, prod.ID, ProductInput{Notes: p("wash before shoot"), SKU: p("")})
	if err != nil {
		t.Fatal(err)
	}
	if up.Notes != "wash before shoot" || up.SKU != "" || up.Name != "Slim jeans" || up.Colour != "indigo" {
		t.Fatalf("updated: %+v", up)
	}
	if !up.UpdatedAt.After(prod.UpdatedAt) && !up.UpdatedAt.Equal(prod.UpdatedAt) {
		t.Fatal("updated_at went backwards")
	}

	if _, err := s.GetProduct(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := s.UpdateProduct(ctx, 999, ProductInput{Notes: p("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
}

func TestValidation(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	var ve *ValidationError

	for name, in := range map[string]ProductInput{
		"no name":    {},
		"blank name": {Name: p("   ")},
		"long name":  {Name: p(strings.Repeat("x", 201))},
		"long sku":   {Name: p("ok"), SKU: p(strings.Repeat("x", 65))},
	} {
		if _, err := s.CreateProduct(ctx, in); !errors.As(err, &ve) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	prod, _ := s.CreateProduct(ctx, ProductInput{Name: p("ok")})
	if _, err := s.UpdateProduct(ctx, prod.ID, ProductInput{Name: p("")}); !errors.As(err, &ve) {
		t.Errorf("clearing name: %v", err)
	}
}

func TestSKUIsUniqueCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	if _, err := s.CreateProduct(ctx, ProductInput{Name: p("a"), SKU: p("ABC")}); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := s.CreateProduct(ctx, ProductInput{Name: p("b"), SKU: p("abc")}); !errors.As(err, &ce) {
		t.Fatalf("duplicate sku: %v", err)
	}
	// Several products without an identifier are fine.
	for range 2 {
		if _, err := s.CreateProduct(ctx, ProductInput{Name: p("no sku")}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAddImageStoresExactBytes(t *testing.T) {
	ctx := context.Background()
	s, _, store := setup(t)
	prod, _ := s.CreateProduct(ctx, ProductInput{Name: p("jeans")})
	data := fixture(t, "rot6.jpg")

	im, err := s.AddProductImage(ctx, prod.ID, "back", `C:\photos\back.jpg`, data)
	if err != nil {
		t.Fatal(err)
	}
	if im.Hash != blob.Hash(data) || im.View != "back" || im.Filename != "back.jpg" ||
		im.ContentType != imaging.TypeJPEG || im.Width != 20 || im.Height != 40 || im.Size != int64(len(data)) {
		t.Fatalf("image: %+v", im)
	}

	o, err := store.Open(ctx, blob.Original, im.Hash)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(o)
	_ = o.Close()
	if string(got) != string(data) {
		t.Fatal("stored bytes differ from upload")
	}

	full, _ := s.GetProduct(ctx, prod.ID)
	if full.ImageCount != 1 || len(full.Images) != 1 || full.Cover == nil || full.Cover.ID != im.ID {
		t.Fatalf("product: %+v", full)
	}
}

func TestAddImageRejects(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	prod, _ := s.CreateProduct(ctx, ProductInput{Name: p("jeans")})

	var ve *ValidationError
	if _, err := s.AddProductImage(ctx, prod.ID, "top", "x.jpg", fixture(t, "plain.jpg")); !errors.As(err, &ve) {
		t.Errorf("bad view: %v", err)
	}
	var ie *imaging.ValidationError
	for _, f := range []string{"plain.gif", "text.jpg", "truncated.jpg"} {
		if _, err := s.AddProductImage(ctx, prod.ID, "front", f, fixture(t, f)); !errors.As(err, &ie) {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := s.AddProductImage(ctx, 999, "front", "x.jpg", fixture(t, "plain.jpg")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing product: %v", err)
	}

	if _, err := s.AddProductImage(ctx, prod.ID, "front", "a.jpg", fixture(t, "plain.jpg")); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := s.AddProductImage(ctx, prod.ID, "back", "a.jpg", fixture(t, "plain.jpg")); !errors.As(err, &ce) {
		t.Errorf("duplicate photo: %v", err)
	}
}

func TestImagesAreImmutableInTheDatabase(t *testing.T) {
	ctx := context.Background()
	s, d, _ := setup(t)
	prod, _ := s.CreateProduct(ctx, ProductInput{Name: p("jeans")})
	im, _ := s.AddProductImage(ctx, prod.ID, "front", "a.jpg", fixture(t, "plain.jpg"))

	for _, q := range []string{
		`UPDATE product_image SET view = 'back' WHERE id = ?`,
		`UPDATE product_image SET blob_hash = 'x' WHERE id = ?`,
	} {
		if _, err := d.Write.ExecContext(ctx, q, im.ID); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Errorf("%s: %v", q, err)
		}
	}
	if _, err := d.Write.ExecContext(ctx, `UPDATE blob SET size = 1 WHERE hash = ?`, im.Hash); err == nil {
		t.Error("blob row updated")
	}
}

func TestDeleteImageCollectsUnreferencedBytes(t *testing.T) {
	ctx := context.Background()
	s, _, store := setup(t)
	a, _ := s.CreateProduct(ctx, ProductInput{Name: p("a")})
	b, _ := s.CreateProduct(ctx, ProductInput{Name: p("b")})
	data := fixture(t, "plain.png")

	imA, _ := s.AddProductImage(ctx, a.ID, "front", "x.png", data)
	imB, err := s.AddProductImage(ctx, b.ID, "front", "x.png", data)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutDerived(ctx, imA.Hash, "w256.jpg", strings.NewReader("thumb")); err != nil {
		t.Fatal(err)
	}

	// Still used by product b: bytes stay.
	if err := s.DeleteProductImage(ctx, a.ID, imA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(ctx, blob.Original, imA.Hash); err != nil {
		t.Fatalf("shared blob removed: %v", err)
	}

	// Wrong product id: not found, nothing removed.
	if err := s.DeleteProductImage(ctx, a.ID, imB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-product delete: %v", err)
	}

	// Last reference gone: bytes and thumbnails go too.
	if err := s.DeleteProduct(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(ctx, blob.Original, imA.Hash); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("orphan blob kept: %v", err)
	}
	if _, err := store.OpenDerived(ctx, imA.Hash, "w256.jpg"); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("orphan thumbnail kept: %v", err)
	}
	if _, err := s.GetProduct(ctx, b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("product survived delete")
	}
	if err := s.DeleteProduct(ctx, b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestListCovers(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)
	a, _ := s.CreateProduct(ctx, ProductInput{Name: p("a")})
	_, _ = s.CreateProduct(ctx, ProductInput{Name: p("empty")})
	_, _ = s.AddProductImage(ctx, a.ID, "detail", "d.jpg", fixture(t, "plain.jpg"))
	front, _ := s.AddProductImage(ctx, a.ID, "front", "f.png", fixture(t, "plain.png"))

	list, err := s.ListProducts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "empty" || list[0].Cover != nil {
		t.Fatalf("list: %+v", list)
	}
	if list[1].ImageCount != 2 || list[1].Cover == nil || list[1].Cover.ID != front.ID {
		t.Fatalf("cover: %+v", list[1])
	}
}

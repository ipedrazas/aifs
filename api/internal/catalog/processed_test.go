package catalog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/blob"
)

func TestProcessedVersions(t *testing.T) {
	ctx := context.Background()
	s, _, store := setup(t)
	prod, _ := s.CreateProduct(ctx, ProductInput{Name: p("Jeans")})
	front, err := s.AddProductImage(ctx, prod.ID, "front", "front.jpg", fixture(t, "plain.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	back, _ := s.AddProductImage(ctx, prod.ID, "back", "back.jpg", fixture(t, "rot6.jpg"))

	proc := Processing{Steps: []string{StepRemoveBackground}, Provider: "fake", CostMicros: 500}
	v1, err := s.AddProcessedImage(ctx, prod.ID, front.ID, fixture(t, "plain.png"), proc)
	if err != nil {
		t.Fatal(err)
	}
	if v1.SourceImageID == nil || *v1.SourceImageID != front.ID || v1.View != "front" || v1.Filename != "front-processed.png" ||
		v1.Processing == nil || v1.Processing.Provider != "fake" || v1.Processing.CostMicros != 500 {
		t.Fatalf("processed: %+v", v1)
	}

	got, _ := s.GetProduct(ctx, prod.ID)
	if got.ImageCount != 2 || len(got.Images) != 3 {
		t.Fatalf("count %d, images %d", got.ImageCount, len(got.Images))
	}
	gen := got.GenerationImages()
	if len(gen) != 2 || gen[0].ID != v1.ID || gen[1].ID != back.ID {
		t.Fatalf("generation images: %+v", gen)
	}
	if got.Cover.ID != v1.ID {
		t.Fatalf("cover: %d", got.Cover.ID)
	}
	list, _ := s.ListProducts(ctx)
	if list[0].Cover.ID != v1.ID || list[0].ImageCount != 2 {
		t.Fatalf("list cover: %+v", list[0].Cover)
	}
	for _, im := range got.Images {
		if im.ID == front.ID && (im.ProcessedImageID == nil || *im.ProcessedImageID != v1.ID) {
			t.Fatalf("original must point at its processed version: %+v", im)
		}
	}

	// The original is untouched.
	_, data, err := s.ReadProductImage(ctx, prod.ID, front.ID)
	if err != nil || !bytes.Equal(data, fixture(t, "plain.jpg")) {
		t.Fatalf("original changed: %v", err)
	}

	// A processed version cannot be processed again.
	var ve *ValidationError
	if _, err := s.AddProcessedImage(ctx, prod.ID, v1.ID, fixture(t, "plain.webp"), proc); !errors.As(err, &ve) {
		t.Fatalf("processing a processed version: %v", err)
	}

	// Processing again replaces the previous version and frees its bytes.
	v2, err := s.AddProcessedImage(ctx, prod.ID, front.ID, fixture(t, "plain.webp"), proc)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProduct(ctx, prod.ID)
	if len(got.Images) != 3 || got.GenerationImages()[0].Hash != v2.Hash {
		t.Fatalf("old version kept: %+v", got.Images)
	}
	if _, err := store.Open(ctx, blob.Original, v1.Hash); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("old bytes kept: %v", err)
	}

	// Deleting the processed version reverts to the original.
	if err := s.DeleteProductImage(ctx, prod.ID, v2.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProduct(ctx, prod.ID)
	if gen := got.GenerationImages(); len(gen) != 2 || gen[0].ID != front.ID || gen[0].ProcessedImageID != nil {
		t.Fatalf("after revert: %+v", gen)
	}

	// Deleting an original deletes its processed version and both files.
	v3, _ := s.AddProcessedImage(ctx, prod.ID, front.ID, fixture(t, "plain.png"), proc)
	if err := s.DeleteProductImage(ctx, prod.ID, front.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProduct(ctx, prod.ID)
	if len(got.Images) != 1 || got.Images[0].ID != back.ID {
		t.Fatalf("after deleting the original: %+v", got.Images)
	}
	for _, h := range []string{front.Hash, v3.Hash} {
		if o, err := store.Open(ctx, blob.Original, h); err == nil {
			_, _ = io.Copy(io.Discard, o)
			_ = o.Close()
			t.Fatalf("bytes kept: %s", h)
		}
	}

	if _, err := s.AddProcessedImage(ctx, prod.ID, 9999, fixture(t, "plain.png"), proc); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown source: %v", err)
	}
}

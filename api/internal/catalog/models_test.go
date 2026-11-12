package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/db"
)

func b(v bool) *bool { return &v }

// referenceModel simulates a later table (generation, photoshoot) that
// refers to a model with ON DELETE RESTRICT.
func referenceModel(t *testing.T, d *db.DB, modelID int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := d.Write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS test_model_ref (
		model_id INTEGER NOT NULL REFERENCES model (id) ON DELETE RESTRICT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write.ExecContext(ctx, `INSERT INTO test_model_ref (model_id) VALUES (?)`, modelID); err != nil {
		t.Fatal(err)
	}
}

func TestModelCRUD(t *testing.T) {
	ctx := context.Background()
	s, _, _ := setup(t)

	m, err := s.CreateModel(ctx, ModelInput{
		Name:           p(" Ava "),
		Description:    p("Friendly, relaxed"),
		Appearance:     p("tall, short dark hair"),
		PreferredPoses: p("walking, hands in pockets"),
		Styling:        p("white trainers"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "Ava" || m.Origin != OriginUploaded || m.Archived || m.ArchivedAt != nil || m.Images == nil {
		t.Fatalf("created: %+v", m)
	}

	up, err := s.UpdateModel(ctx, m.ID, ModelInput{Styling: p("black boots")})
	if err != nil || up.Styling != "black boots" || up.Appearance != "tall, short dark hair" {
		t.Fatalf("update: %+v %v", up, err)
	}

	var ve *ValidationError
	if _, err := s.CreateModel(ctx, ModelInput{}); !errors.As(err, &ve) {
		t.Fatalf("missing name: %v", err)
	}
	if _, err := s.CreateModel(ctx, ModelInput{Name: p("x"), Archived: b(true)}); !errors.As(err, &ve) {
		t.Fatalf("archived on create: %v", err)
	}
	if _, err := s.UpdateModel(ctx, 999, ModelInput{Name: p("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	list, _ := s.ListModels(ctx, false)
	if len(list) != 1 || list[0].ID != m.ID {
		t.Fatalf("list: %+v", list)
	}

	deleted, err := s.DeleteModel(ctx, m.ID)
	if err != nil || !deleted {
		t.Fatalf("delete unused model: %v %v", deleted, err)
	}
	if _, err := s.GetModel(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("model survived delete")
	}
	if _, err := s.DeleteModel(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestModelImages(t *testing.T) {
	ctx := context.Background()
	s, _, store := setup(t)
	m, _ := s.CreateModel(ctx, ModelInput{Name: p("Ava")})

	full, err := s.AddModelImage(ctx, m.ID, "full_body", "full.png", fixture(t, "plain.png"))
	if err != nil {
		t.Fatal(err)
	}
	face, err := s.AddModelImage(ctx, m.ID, "identity", "face.jpg", fixture(t, "rot6.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if face.Role != "identity" || face.Width != 20 || face.Height != 40 {
		t.Fatalf("image: %+v", face)
	}

	got, _ := s.GetModel(ctx, m.ID)
	if got.ImageCount != 2 || got.Images[0].ID != face.ID || got.Cover == nil || got.Cover.ID != face.ID {
		t.Fatalf("identity first: %+v", got.Images)
	}
	list, _ := s.ListModels(ctx, false)
	if list[0].Cover == nil || list[0].Cover.ID != face.ID {
		t.Fatalf("list cover: %+v", list[0].Cover)
	}

	var ve *ValidationError
	if _, err := s.AddModelImage(ctx, m.ID, "headshot", "x.jpg", fixture(t, "plain.jpg")); !errors.As(err, &ve) {
		t.Fatalf("bad role: %v", err)
	}
	var ce *ConflictError
	if _, err := s.AddModelImage(ctx, m.ID, "identity", "again.png", fixture(t, "plain.png")); !errors.As(err, &ce) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.AddModelImage(ctx, 999, "identity", "x.jpg", fixture(t, "plain.jpg")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing model: %v", err)
	}

	if err := s.DeleteModelImage(ctx, m.ID, full.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(ctx, blob.Original, full.Hash); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("unreferenced bytes kept: %v", err)
	}
}

func TestModelImagesAreImmutable(t *testing.T) {
	ctx := context.Background()
	s, d, _ := setup(t)
	m, _ := s.CreateModel(ctx, ModelInput{Name: p("Ava")})
	im, _ := s.AddModelImage(ctx, m.ID, "identity", "a.jpg", fixture(t, "plain.jpg"))
	_, err := d.Write.ExecContext(ctx, `UPDATE model_image SET role = 'full_body' WHERE id = ?`, im.ID)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("update allowed: %v", err)
	}
}

func TestBlobsSharedBetweenProductsAndModels(t *testing.T) {
	ctx := context.Background()
	s, _, store := setup(t)
	prod, _ := s.CreateProduct(ctx, ProductInput{Name: p("jeans")})
	m, _ := s.CreateModel(ctx, ModelInput{Name: p("Ava")})
	data := fixture(t, "plain.png")

	if _, err := s.AddProductImage(ctx, prod.ID, "front", "a.png", data); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddModelImage(ctx, m.ID, "full_body", "a.png", data); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteModel(ctx, m.ID); err != nil || !deleted {
		t.Fatalf("delete model: %v %v", deleted, err)
	}
	// The product still uses the bytes.
	if _, err := store.Open(ctx, blob.Original, blob.Hash(data)); err != nil {
		t.Fatalf("shared bytes removed: %v", err)
	}
}

func TestReferencedModelIsArchivedNotDeleted(t *testing.T) {
	ctx := context.Background()
	s, d, store := setup(t)
	m, _ := s.CreateModel(ctx, ModelInput{Name: p("Ava")})
	im, _ := s.AddModelImage(ctx, m.ID, "identity", "a.jpg", fixture(t, "plain.jpg"))
	referenceModel(t, d, m.ID)

	deleted, err := s.DeleteModel(ctx, m.ID)
	if err != nil || deleted {
		t.Fatalf("referenced model: deleted=%v err=%v", deleted, err)
	}
	got, err := s.GetModel(ctx, m.ID)
	if err != nil || !got.Archived || got.ArchivedAt == nil {
		t.Fatalf("not archived: %+v %v", got, err)
	}
	// Images and bytes are kept for traceability.
	if len(got.Images) != 1 {
		t.Fatalf("images lost: %+v", got.Images)
	}
	if _, err := store.Open(ctx, blob.Original, im.Hash); err != nil {
		t.Fatalf("bytes lost: %v", err)
	}

	// Hidden from the default list, visible with archived included.
	if list, _ := s.ListModels(ctx, false); len(list) != 0 {
		t.Fatalf("archived model listed: %+v", list)
	}
	if list, _ := s.ListModels(ctx, true); len(list) != 1 || !list[0].Archived {
		t.Fatalf("archived model missing from full list: %+v", list)
	}

	// Archiving again keeps the original timestamp; restoring clears it.
	first := *got.ArchivedAt
	if _, err := s.DeleteModel(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetModel(ctx, m.ID)
	if !again.ArchivedAt.Equal(first) {
		t.Fatal("archive timestamp changed")
	}
	restored, err := s.UpdateModel(ctx, m.ID, ModelInput{Archived: b(false)})
	if err != nil || restored.Archived {
		t.Fatalf("restore: %+v %v", restored, err)
	}
}

package studio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/provider"
	"github.com/ipedrazas/aifs/api/internal/provider/fake"
	"github.com/ipedrazas/aifs/api/internal/provider/fashn"
)

func TestTryOnPhotoRecipe(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "tuck in")
	full, err := e.cat.AddModelImage(ctx, e.model.ID, "full_body", "full.png", fixture(t, "plain.png"))
	if err != nil {
		t.Fatal(err)
	}
	prod, _ := e.cat.GetProduct(ctx, e.product.ID)
	back := prod.Images[1] // front, back
	if back.View != "back" {
		t.Fatalf("fixture order: %+v", prod.Images)
	}
	e.start()

	g, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{
		Recipe: "tryon-photo", Provider: fake.Name, Variations: 1, GarmentImageID: back.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	g = e.waitStatus(g.ID, StatusSucceeded)

	st := g.Settings.Steps[0]
	if st.Capability != "try_on" || st.Prompt != "tuck in" || len(st.Inputs) != 1 {
		t.Fatalf("step: %+v", st)
	}
	// The chosen garment photograph and the model's full-body photograph
	// are recorded as the inputs.
	garment := g.Settings.Inputs[st.Inputs[0]-1]
	source := g.Settings.Inputs[st.Source-1]
	if garment.Hash != back.Hash || garment.Label != "back" {
		t.Fatalf("garment input: %+v", garment)
	}
	if source.Hash != full.Hash || source.Label != "full_body" {
		t.Fatalf("source input: %+v", source)
	}

	var ve *ValidationError
	other, _ := e.cat.CreateProduct(ctx, catalog.ProductInput{Name: ptr("other")})
	im, _ := e.cat.AddProductImage(ctx, other.ID, "front", "o.jpg", fixture(t, "plain.jpg"))
	if _, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{
		Recipe: "tryon-photo", Provider: fake.Name, Variations: 1, GarmentImageID: im.ID,
	}); !errors.As(err, &ve) {
		t.Fatalf("garment from another product: %v", err)
	}
}

// The FASHN adapter end to end through the durable runner, against replayed
// API responses.
func TestFASHNThroughRunner(t *testing.T) {
	ctx := context.Background()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "provider", "fashn", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var polls atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			_, _ = w.Write(read("output.png"))
		case r.URL.Path == "/v1/run":
			_, _ = w.Write(read("run_ok.json"))
		case strings.HasPrefix(r.URL.Path, "/v1/status/"):
			if polls.Add(1) < 3 {
				_, _ = w.Write(read("status_processing.json"))
				return
			}
			w.Header().Set("x-fashn-credits-used", "2")
			_, _ = w.Write([]byte(strings.ReplaceAll(string(read("status_completed.json")), "{{CDN}}", srv.URL+"/cdn")))
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	e := newEnv(t, 0, "")
	fp, err := fashn.New(fashn.Config{APIKey: "k", BaseURL: srv.URL, OutputHosts: []string{u.Hostname()}, Resolution: "1k"})
	if err != nil {
		t.Fatal(err)
	}
	e.reg = provider.NewRegistry()
	e.reg.Add(fp, fp.UnitPriceMicros())
	e.runner = NewRunner(e.db, e.blobs, e.reg, e.log, fastRunner)
	e.svc = New(e.db, e.blobs, e.cat, e.reg, e.runner, e.log, Config{MaxVariations: 4})
	e.start()

	// FASHN cannot run the recipes that need a generated scene.
	var ve *ValidationError
	if _, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{Recipe: "reference", Provider: fashn.Name, Variations: 1}); !errors.As(err, &ve) {
		t.Fatalf("reference recipe on FASHN: %v", err)
	}

	g, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{Recipe: "tryon-photo", Provider: fashn.Name, Variations: 1})
	if err != nil {
		t.Fatal(err)
	}
	g = e.waitStatus(g.ID, StatusSucceeded)
	if len(g.Images) != 1 || g.CostMicros != 2*75_000 {
		t.Fatalf("generation: images=%d cost=%d", len(g.Images), g.CostMicros)
	}
	st := g.Steps[0]
	if st.ProviderJobID != "123a87r9-4129-4bb3-be18-9c9fb5bd7fc1-u1" || st.ProviderModel != "tryon-max (1k, balanced)" {
		t.Fatalf("step: %+v", st)
	}
	// With no full-body image the model's identity image is dressed.
	if src := g.Settings.Inputs[g.Settings.Steps[0].Source-1]; src.Role != "model" {
		t.Fatalf("source: %+v", src)
	}
}

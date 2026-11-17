package studio

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/provider"
	"github.com/ipedrazas/aifs/api/internal/provider/fake"
)

// recorder keeps every request a provider receives.
type recorder struct {
	provider.Provider
	mu   sync.Mutex
	reqs []provider.Request
}

func (r *recorder) Name() string { return "recorder" }

func (r *recorder) Submit(ctx context.Context, req provider.Request) (string, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.mu.Unlock()
	return r.Provider.Submit(ctx, req)
}

func wide(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4000, 40))
	for x := range 4000 {
		for y := range 40 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y * 6), 90, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGenerationUsesProcessedVersions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, "")
	rec := &recorder{Provider: fake.New(0)}
	e.reg.Add(rec, 0)

	prod, _ := e.cat.GetProduct(ctx, e.product.ID)
	front := prod.Images[0]
	processed, err := e.cat.AddProcessedImage(ctx, e.product.ID, front.ID, wide(t), catalog.Processing{Steps: []string{catalog.StepRemoveBackground}})
	if err != nil {
		t.Fatal(err)
	}
	e.start()

	g, err := e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{Recipe: "reference", Provider: "recorder", Variations: 1})
	if err != nil {
		t.Fatal(err)
	}
	snap := g.Settings
	if len(snap.Inputs) != 3 || snap.Inputs[0].Hash != processed.Hash || !snap.Inputs[0].Processed || snap.Inputs[1].Processed {
		t.Fatalf("inputs: %+v", snap.Inputs)
	}
	e.waitStatus(g.ID, StatusSucceeded)

	// The 4000 px processed photograph is sent scaled to 3072 px; the WebP
	// back view is converted to JPEG; nothing stored changes.
	req := rec.reqs[0]
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(req.Garments[0].Data))
	if err != nil || cfg.Width != 3072 || req.Garments[0].ContentType != "image/jpeg" || req.Garments[0].Hash != processed.Hash {
		t.Fatalf("sent: %dx%d %s %v", cfg.Width, cfg.Height, req.Garments[0].ContentType, err)
	}
	if req.Garments[1].ContentType != "image/jpeg" {
		t.Fatalf("back view: %s", req.Garments[1].ContentType)
	}

	// Choosing the original explicitly sends it instead of its processed version.
	g, err = e.svc.StartGeneration(ctx, e.shoot.ID, GenerationRequest{Recipe: "reference", Provider: "recorder", Variations: 1, GarmentImageID: front.ID})
	if err != nil {
		t.Fatal(err)
	}
	if in := g.Settings.Inputs; in[0].Hash != front.Hash || in[0].Processed || in[1].Role != "garment" || in[1].Hash == processed.Hash || in[2].Role != "model" {
		t.Fatalf("explicit original: %+v", in)
	}
	e.waitStatus(g.ID, StatusSucceeded)

	sum, err := e.svc.MetricsSummary(ctx, MetricsFilter{GroupBy: "garmentProcessing"})
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]int{}
	for _, gr := range sum.Groups {
		labels[gr.Label] = gr.Generations
	}
	if labels["original and processed photos"] != 1 || labels["original photos"] != 1 || len(labels) != 2 {
		t.Fatalf("groups: %v", labels)
	}
}

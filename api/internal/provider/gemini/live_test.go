//go:build live

// Live check against the real OpenRouter API. It costs money, so it only
// runs with the "live" build tag and an API key:
//
//	AIFS_OPENROUTER_API_KEY=... go test -tags live -run TestLive -v ./internal/provider/gemini/
//
// Optional: GEMINI_MODEL overrides the model, GEMINI_GARMENT and GEMINI_PERSON
// point at your own photographs, and the result is written to GEMINI_OUT
// (default: a temp file).
package gemini

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

func TestLiveReferenceGenerate(t *testing.T) {
	key := os.Getenv("AIFS_OPENROUTER_API_KEY")
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		t.Skip("AIFS_OPENROUTER_API_KEY not set")
	}
	read := func(env, fallback string) []byte {
		path := os.Getenv(env)
		if path == "" {
			path = filepath.Join("..", "..", "imaging", "testdata", fallback)
		}
		b, err := os.ReadFile(path) //nolint:gosec // test input
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	p, err := New(Config{APIKey: key, Model: os.Getenv("GEMINI_MODEL"), Resolution: "1K"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	id, err := p.Submit(ctx, provider.Request{
		Capability:  provider.ReferenceGenerate,
		Prompt:      "Professional e-commerce fashion photograph of the person in the model reference wearing the garment, plain light-grey studio backdrop.",
		AspectRatio: "3:4",
		Garments:    []provider.Image{{Role: "garment", Label: "front", Data: read("GEMINI_GARMENT", "plain.png")}},
		ModelRefs:   []provider.Image{{Role: "model", Label: "identity", Data: read("GEMINI_PERSON", "rot6.jpg")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Poll(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != provider.Succeeded {
		t.Fatalf("result: %s", res.Error)
	}
	out := os.Getenv("GEMINI_OUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), "gemini.png")
	}
	if err := os.WriteFile(out, res.Images[0].Data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("image: %s (%d bytes, %s), cost reported=%v %d micro-USD", out, len(res.Images[0].Data), res.Images[0].ContentType, res.CostReported, res.CostMicros)
}

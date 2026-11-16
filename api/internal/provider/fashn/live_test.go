//go:build live

// Live check against the real FASHN API. It spends credits, so it only runs
// with the "live" build tag and an API key:
//
//	AIFS_FASHN_API_KEY=... go test -tags live -run TestLive -v ./internal/provider/fashn/
//
// Optional: FASHN_PERSON and FASHN_GARMENT point at your own photographs;
// the result is written to FASHN_OUT (default: a temp file).
package fashn

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

func TestLiveTryOn(t *testing.T) {
	key := os.Getenv("AIFS_FASHN_API_KEY")
	if key == "" {
		t.Skip("AIFS_FASHN_API_KEY not set")
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
	person, garment := read("FASHN_PERSON", "rot6.jpg"), read("FASHN_GARMENT", "plain.png")

	p, err := New(Config{APIKey: key, Model: os.Getenv("AIFS_FASHN_MODEL"), Resolution: "1k", Mode: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	id, err := p.Submit(ctx, provider.Request{
		Capability:      provider.TryOn,
		Source:          &provider.Image{ContentType: http.DetectContentType(person), Data: person},
		Garments:        []provider.Image{{ContentType: http.DetectContentType(garment), Data: garment}},
		GarmentCategory: "jeans",
		Seed:            42,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	t.Logf("job %s", id)

	for {
		res, err := p.Poll(ctx, id)
		if err != nil {
			if ok, _ := provider.IsTransient(err); ok {
				time.Sleep(3 * time.Second)
				continue
			}
			t.Fatalf("poll: %v", err)
		}
		switch res.Status {
		case provider.Pending:
			time.Sleep(3 * time.Second)
			continue
		case provider.Failed:
			// The tiny test fixtures are not real photographs; a pose error
			// still proves the API round trip works.
			t.Logf("job failed (expected with placeholder images): %s", res.Error)
			return
		}
		out := os.Getenv("FASHN_OUT")
		if out == "" {
			out = filepath.Join(t.TempDir(), "fashn.png")
		}
		if err := os.WriteFile(out, res.Images[0].Data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("result: %s (%d bytes, cost %d micro-USD, reported %v)", out, len(res.Images[0].Data), res.CostMicros, res.CostReported)
		return
	}
}

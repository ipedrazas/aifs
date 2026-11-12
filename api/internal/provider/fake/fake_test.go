package fake

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "imaging", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func request(t *testing.T, prompt string, seed int64) provider.Request {
	return provider.Request{
		Capability:  provider.ReferenceGenerate,
		Prompt:      prompt,
		Garments:    []provider.Image{{Role: "garment", Data: fixture(t, "plain.png")}},
		ModelRefs:   []provider.Image{{Role: "model", Data: fixture(t, "rot6.jpg")}},
		AspectRatio: "3:4",
		Seed:        seed,
	}
}

func TestSubmitPollProducesDeterministicImage(t *testing.T) {
	ctx := context.Background()
	p := New(0)

	run := func(seed int64) []byte {
		id, err := p.Submit(ctx, request(t, "p", seed))
		if err != nil {
			t.Fatal(err)
		}
		res, err := p.Poll(ctx, id)
		if err != nil || res.Status != provider.Succeeded || len(res.Images) != 1 || !res.CostReported {
			t.Fatalf("poll: %+v %v", res, err)
		}
		return res.Images[0].Data
	}
	a, b, c := run(1), run(1), run(2)
	if !bytes.Equal(a, b) {
		t.Fatal("same seed, different image")
	}
	if bytes.Equal(a, c) {
		t.Fatal("different seeds, same image")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(a))
	if err != nil || cfg.Width != 768 || cfg.Height != 1024 {
		t.Fatalf("3:4 canvas: %+v %v", cfg, err)
	}
}

func TestPendingUntilDelay(t *testing.T) {
	ctx := context.Background()
	p := New(time.Minute)
	now := time.Unix(0, 0)
	p.now = func() time.Time { return now }

	id, _ := p.Submit(ctx, request(t, "p", 1))
	if res, _ := p.Poll(ctx, id); res.Status != provider.Pending {
		t.Fatalf("got %s", res.Status)
	}
	now = now.Add(time.Minute)
	if res, _ := p.Poll(ctx, id); res.Status != provider.Succeeded {
		t.Fatalf("got %s", res.Status)
	}
	// A finished job is forgotten, like a provider expiring results.
	if _, err := p.Poll(ctx, id); !errors.Is(err, provider.ErrUnknownJob) {
		t.Fatalf("got %v", err)
	}
}

func TestSimulatedFailures(t *testing.T) {
	ctx := context.Background()
	p := New(0)

	id, _ := p.Submit(ctx, request(t, "x [fake:fail]", 1))
	if res, _ := p.Poll(ctx, id); res.Status != provider.Failed || res.Error == "" {
		t.Fatalf("fail: %+v", res)
	}

	if _, err := p.Submit(ctx, request(t, "[fake:reject]", 1)); err == nil {
		t.Fatal("reject: no error")
	} else if ok, _ := provider.IsTransient(err); ok {
		t.Fatal("reject must be permanent")
	}

	_, err := p.Submit(ctx, request(t, "[fake:flaky]", 7))
	if ok, _ := provider.IsTransient(err); !ok {
		t.Fatalf("flaky first call: %v", err)
	}
	if _, err := p.Submit(ctx, request(t, "[fake:flaky]", 7)); err != nil {
		t.Fatalf("flaky retry: %v", err)
	}

	if _, err := p.Poll(ctx, "nope"); !errors.Is(err, provider.ErrUnknownJob) {
		t.Fatalf("unknown job: %v", err)
	}
}

func TestTryOnNeedsInputs(t *testing.T) {
	req := request(t, "", 1)
	req.Capability = provider.TryOn
	if _, err := New(0).Submit(context.Background(), req); err == nil {
		t.Fatal("try-on without a source accepted")
	}
	req.Source = &provider.Image{Data: fixture(t, "plain.jpg")}
	if _, err := New(0).Submit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasSize(t *testing.T) {
	for in, want := range map[string][2]int{"1:1": {1024, 1024}, "16:9": {1024, 576}, "9:16": {576, 1024}, "bad": {768, 1024}} {
		w, h := canvasSize(in)
		if w != want[0] || h != want[1] {
			t.Errorf("%s: %dx%d", in, w, h)
		}
	}
}

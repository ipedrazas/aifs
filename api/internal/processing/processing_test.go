package processing

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/imaging"
	"github.com/ipedrazas/aifs/api/internal/provider"
	"github.com/ipedrazas/aifs/api/internal/provider/fake"
)

type env struct {
	t    *testing.T
	cat  *catalog.Service
	reg  *provider.Registry
	svc  *Service
	prod catalog.Product
}

func newEnv(t *testing.T, providers ...provider.Provider) *env {
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
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cat := catalog.New(d, store, log)
	reg := provider.NewRegistry()
	for _, p := range providers {
		reg.Add(p, 500)
	}
	name := "Jeans"
	prod, err := cat.CreateProduct(context.Background(), catalog.ProductInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, cat: cat, reg: reg, prod: prod,
		svc: New(cat, reg, log, Config{PollInterval: time.Millisecond, Timeout: 5 * time.Second})}
}

// photo is a 600×800 garment photograph: a patterned 200×400 garment at
// (200,200) on a flat light background.
func photo() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 600, 800))
	for y := range 800 {
		for x := range 600 {
			c := color.NRGBA{230, 228, 220, 255}
			if x >= 200 && x < 400 && y >= 200 && y < 600 {
				c = color.NRGBA{30, 50, uint8(100 + (x+y)%100), 255}
				if (x/10+y/10)%2 == 0 {
					c = color.NRGBA{200, 40, 40, 255}
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func (e *env) upload(img image.Image) catalog.ProductImage {
	e.t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		e.t.Fatal(err)
	}
	im, err := e.cat.AddProductImage(context.Background(), e.prod.ID, "front", "front.png", buf.Bytes())
	if err != nil {
		e.t.Fatal(err)
	}
	return im
}

func (e *env) decode(im catalog.ProductImage) *image.NRGBA {
	e.t.Helper()
	_, data, err := e.cat.ReadProductImage(context.Background(), e.prod.ID, im.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	img, err := imaging.Decode(data)
	if err != nil {
		e.t.Fatal(err)
	}
	return img
}

func TestRemoveBackgroundTrimResize(t *testing.T) {
	e := newEnv(t, fake.New(0))
	ctx := context.Background()
	src := photo()
	orig := e.upload(src)

	// Background removal alone keeps every garment pixel exactly.
	im, err := e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true})
	if err != nil {
		t.Fatal(err)
	}
	if im.ContentType != imaging.TypePNG || im.Width != 600 || im.Height != 800 {
		t.Fatalf("cut-out: %+v", im)
	}
	out := e.decode(im)
	if out.NRGBAAt(10, 10).A != 0 {
		t.Fatal("background kept")
	}
	for _, p := range []image.Point{{200, 200}, {300, 400}, {399, 599}, {246, 374}} {
		if out.NRGBAAt(p.X, p.Y) != src.NRGBAAt(p.X, p.Y) {
			t.Fatalf("garment pixel at %v changed: %v → %v", p, src.NRGBAAt(p.X, p.Y), out.NRGBAAt(p.X, p.Y))
		}
	}
	pr := im.Processing
	if pr == nil || len(pr.Steps) != 1 || pr.Provider != fake.Name || pr.ProviderModel != "fake-remove_background-v1" ||
		pr.ProviderJobID == "" || pr.CostMicros != fake.CostMicros {
		t.Fatalf("record: %+v", pr)
	}

	// All three steps; this replaces the previous version.
	im, err = e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true, Trim: true, MaxEdge: 256})
	if err != nil {
		t.Fatal(err)
	}
	// Trimmed to 200×400 plus a 3% margin of 400 (12 px) each side, then
	// scaled from 224×424 to a 256 px long edge.
	if im.Width != 135 || im.Height != 256 {
		t.Fatalf("trim and resize: %dx%d", im.Width, im.Height)
	}
	if got := strings.Join(im.Processing.Steps, ","); got != "remove_background,trim,resize" || im.Processing.MaxEdge != 256 || im.Processing.TrimMarginPct != 3 {
		t.Fatalf("steps: %+v", im.Processing)
	}
	prod, _ := e.cat.GetProduct(ctx, e.prod.ID)
	if gen := prod.GenerationImages(); len(gen) != 1 || gen[0].ID != im.ID {
		t.Fatalf("generation must use the processed version: %+v", gen)
	}

	// A resize that changes nothing is not recorded.
	im, err = e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true, MaxEdge: 4096})
	if err != nil || strings.Join(im.Processing.Steps, ",") != "remove_background" || im.Processing.MaxEdge != 0 {
		t.Fatalf("no-op resize: %+v %v", im.Processing, err)
	}

	// The original is untouched.
	_, data, _ := e.cat.ReadProductImage(ctx, e.prod.ID, orig.ID)
	if blob.Hash(data) != orig.Hash {
		t.Fatal("original changed")
	}
}

func TestResizeOnly(t *testing.T) {
	e := newEnv(t)
	orig := e.upload(photo())
	im, err := e.svc.Process(context.Background(), e.prod.ID, orig.ID, Options{MaxEdge: 300})
	if err != nil {
		t.Fatal(err)
	}
	if im.ContentType != imaging.TypeJPEG || im.Width != 225 || im.Height != 300 || im.Processing.Provider != "" || im.Processing.CostMicros != 0 {
		t.Fatalf("%+v %+v", im, im.Processing)
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	orig := e.upload(photo())
	for name, c := range map[string]struct {
		opt   Options
		field string
	}{
		"no steps":           {Options{}, "steps"},
		"tiny edge":          {Options{MaxEdge: 10}, "maxEdge"},
		"already small":      {Options{MaxEdge: 1000}, "maxEdge"},
		"opaque trim":        {Options{Trim: true}, "trim"},
		"no remover":         {Options{RemoveBackground: true}, "removeBackground"},
		"unknown provider":   {Options{RemoveBackground: true, Provider: "nope"}, "provider"},
		"provider cannot do": {Options{RemoveBackground: true, Provider: "stub"}, "provider"},
	} {
		if name == "provider cannot do" {
			e.reg.Add(&stub{caps: []provider.Capability{provider.TryOn}}, 0)
		}
		_, err := e.svc.Process(ctx, e.prod.ID, orig.ID, c.opt)
		var ve *catalog.ValidationError
		if !errors.As(err, &ve) || ve.Fields[c.field] == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true}); err == nil || !strings.Contains(err.Error(), "AIFS_REPLICATE_API_TOKEN") {
		t.Errorf("no remover message: %v", err)
	}
	if _, err := e.svc.Process(ctx, e.prod.ID, 999, Options{MaxEdge: 300}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("unknown image: %v", err)
	}
}

func TestTrimsAlreadyTransparentPhotograph(t *testing.T) {
	e := newEnv(t)
	img := photo()
	for y := range 800 {
		for x := range 600 {
			if x < 200 || x >= 400 || y < 200 || y >= 600 {
				img.SetNRGBA(x, y, color.NRGBA{})
			}
		}
	}
	orig := e.upload(img)
	im, err := e.svc.Process(context.Background(), e.prod.ID, orig.ID, Options{Trim: true})
	if err != nil || im.Width != 224 || im.Height != 424 {
		t.Fatalf("%+v %v", im, err)
	}
}

// stub is a scripted background remover.
type stub struct {
	caps    []provider.Capability
	mu      sync.Mutex
	submits int
	// submitErrs are returned by successive Submit calls (nil = accept).
	submitErrs []error
	result     provider.Result
}

func (s *stub) Name() string  { return "stub" }
func (s *stub) Label() string { return "Stub remover" }
func (s *stub) Capabilities() []provider.Capability {
	if s.caps != nil {
		return s.caps
	}
	return []provider.Capability{provider.RemoveBackground}
}
func (s *stub) Model(provider.Capability) string { return "stub-v1" }
func (s *stub) Submit(context.Context, provider.Request) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submits++
	if len(s.submitErrs) > 0 {
		err := s.submitErrs[0]
		s.submitErrs = s.submitErrs[1:]
		if err != nil {
			return "", err
		}
	}
	return "job-1", nil
}
func (s *stub) Poll(context.Context, string) (provider.Result, error) { return s.result, nil }

func TestProviderFailures(t *testing.T) {
	ctx := context.Background()

	failed := &stub{result: provider.Result{Status: provider.Failed, Error: "Replicate could not remove the background (bad image)"}}
	e := newEnv(t, failed)
	orig := e.upload(photo())
	_, err := e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true})
	var pe *ProviderError
	if !errors.As(err, &pe) || !strings.Contains(pe.Msg, "bad image") {
		t.Fatalf("failed job: %v", err)
	}

	permanent := &stub{submitErrs: []error{errors.New("the Replicate account has no credit left")}}
	e = newEnv(t, permanent)
	orig = e.upload(photo())
	if _, err := e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true}); !errors.As(err, &pe) || permanent.submits != 1 {
		t.Fatalf("permanent: %v after %d calls", err, permanent.submits)
	}

	mask, _ := fake.Mask(provider.Request{Source: &provider.Image{Data: encodePNG(t, photo())}})
	flaky := &stub{
		submitErrs: []error{provider.Transient(errors.New("rate limit"), time.Millisecond)},
		result:     provider.Result{Status: provider.Succeeded, Images: []provider.Output{{ContentType: "image/png", Data: mask}}},
	}
	e = newEnv(t, flaky)
	orig = e.upload(photo())
	im, err := e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true})
	if err != nil || flaky.submits != 2 {
		t.Fatalf("transient: %v after %d calls", err, flaky.submits)
	}
	if im.Processing.CostMicros != 500 { // unit price: the stub reports no cost
		t.Fatalf("cost: %d", im.Processing.CostMicros)
	}

	nothing := &stub{result: provider.Result{Status: provider.Succeeded}}
	e = newEnv(t, nothing)
	orig = e.upload(photo())
	if _, err := e.svc.Process(ctx, e.prod.ID, orig.ID, Options{RemoveBackground: true}); !errors.As(err, &pe) {
		t.Fatalf("no mask: %v", err)
	}
}

func TestPrefersRealRemover(t *testing.T) {
	e := newEnv(t, fake.New(0), &stub{})
	if p := e.svc.BackgroundRemover(); p == nil || p.Name() != "stub" {
		t.Fatalf("got %v", p)
	}
	if p := newEnv(t, fake.New(0)).svc.BackgroundRemover(); p == nil || p.Name() != fake.Name {
		t.Fatalf("fallback: %v", p)
	}
	if p := newEnv(t).svc.BackgroundRemover(); p != nil {
		t.Fatalf("none: %v", p)
	}
}

func encodePNG(t *testing.T, img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

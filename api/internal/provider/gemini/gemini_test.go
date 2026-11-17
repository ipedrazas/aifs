package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

const testKey = "or-test-key-123"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func imageFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "imaging", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeAPI replays recorded responses and records requests.
type fakeAPI struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []map[string]any
	status int
	file   string
	header http.Header
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{t: t, status: http.StatusOK, file: "images_ok.json", header: http.Header{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(fixture(f.t, "error_unauthorized.json"))
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/images" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.bodies = append(f.bodies, body)
	status, file := f.status, f.file
	f.mu.Unlock()
	for k, v := range f.header {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(fixture(f.t, file))
}

func (f *fakeAPI) respond(status int, file string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.file = status, file
}

func (f *fakeAPI) provider(t *testing.T, mod func(*Config)) *Provider {
	t.Helper()
	cfg := Config{APIKey: testKey, BaseURL: f.srv.URL + "/api/v1"}
	if mod != nil {
		mod(&cfg)
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func referenceRequest(t *testing.T) provider.Request {
	return provider.Request{
		Capability:  provider.ReferenceGenerate,
		Prompt:      "Professional e-commerce fashion photograph.",
		AspectRatio: "3:4",
		Garments: []provider.Image{
			{Role: "garment", Label: "front", ContentType: "image/png", Data: imageFixture(t, "plain.png")},
			{Role: "garment", Label: "back", ContentType: "image/webp", Data: imageFixture(t, "plain.webp")},
		},
		ModelRefs: []provider.Image{
			{Role: "model", Label: "full_body", ContentType: "image/jpeg", Data: imageFixture(t, "rot6.jpg")},
		},
	}
}

func TestReferenceGenerate(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI(t)
	p := api.provider(t, nil)

	id, err := p.Submit(ctx, referenceRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Poll(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != provider.Succeeded || len(res.Images) != 1 || res.Images[0].ContentType != "image/png" {
		t.Fatalf("result: %+v", res)
	}
	if !bytes.Equal(res.Images[0].Data, fixture(t, "../../fashn/testdata/output.png")) {
		t.Fatal("image bytes differ from the recorded response")
	}
	if !res.CostReported || res.CostMicros != 136_900 {
		t.Fatalf("cost: %+v", res)
	}

	// The result is collected once; afterwards the job is unknown.
	if _, err := p.Poll(ctx, id); !errors.Is(err, provider.ErrUnknownJob) {
		t.Fatalf("second poll: %v", err)
	}

	body := api.bodies[0]
	if body["model"] != DefaultModel || body["aspect_ratio"] != "3:4" || body["resolution"] != "2K" || body["n"] != float64(1) {
		t.Fatalf("request: %v", body)
	}
	refs := body["input_references"].([]any)
	if len(refs) != 3 {
		t.Fatalf("references: %d", len(refs))
	}
	first := refs[0].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(first, "data:image/png;base64,") {
		t.Fatalf("first reference: %.40s", first)
	}
	prompt := body["prompt"].(string)
	for _, want := range []string{
		"1. garment reference photograph (front)",
		"2. garment reference photograph (back)",
		"3. model reference image (full body)",
		"Professional e-commerce fashion photograph.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if _, ok := body["seed"]; ok {
		t.Error("seed is not supported by the Images API and must not be sent")
	}
}

func TestEditSendsSourceFirst(t *testing.T) {
	api := newFakeAPI(t)
	p := api.provider(t, nil)
	req := provider.Request{
		Capability: provider.Edit,
		Prompt:     "Brighten the background.",
		Source:     &provider.Image{Role: "source", ContentType: "image/jpeg", Data: imageFixture(t, "rot6.jpg")},
	}
	if _, err := p.Submit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	body := api.bodies[0]
	if !strings.HasPrefix(body["prompt"].(string), "Attached images, in order:\n1. the photograph to work from\n") {
		t.Fatalf("prompt: %s", body["prompt"])
	}

	req.Source = nil
	if _, err := p.Submit(context.Background(), req); err == nil {
		t.Fatal("an edit without a source must fail")
	}
}

func TestFlashImageModel(t *testing.T) {
	api := newFakeAPI(t)
	p := api.provider(t, func(c *Config) { c.Model = "google/gemini-2.5-flash-image" })
	if p.Model(provider.ReferenceGenerate) != "google/gemini-2.5-flash-image" {
		t.Fatalf("model: %s", p.Model(provider.ReferenceGenerate))
	}
	req := referenceRequest(t)
	if _, err := p.Submit(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, ok := api.bodies[0]["resolution"]; ok {
		t.Error("gemini-2.5-flash-image takes no resolution")
	}
	// It accepts at most three references.
	req.ModelRefs = append(req.ModelRefs, req.ModelRefs[0])
	_, err := p.Submit(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "at most 3") {
		t.Fatalf("too many references: %v", err)
	}
	if len(api.bodies) != 1 {
		t.Fatal("an over-limit request must not reach the API")
	}
}

func TestCostFallback(t *testing.T) {
	api := newFakeAPI(t)
	api.respond(http.StatusOK, "images_ok_no_usage.json")
	p := api.provider(t, nil)
	id, err := p.Submit(context.Background(), referenceRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	res, _ := p.Poll(context.Background(), id)
	if res.CostReported {
		t.Fatalf("cost must not be reported: %+v", res)
	}
	if p.UnitPriceMicros() != DefaultUnitPriceMicros {
		t.Fatal("unit price")
	}
}

func TestNoImageIsAFailedJob(t *testing.T) {
	api := newFakeAPI(t)
	api.respond(http.StatusOK, "images_empty.json")
	p := api.provider(t, nil)
	id, err := p.Submit(context.Background(), referenceRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	res, _ := p.Poll(context.Background(), id)
	if res.Status != provider.Failed || res.Error == "" {
		t.Fatalf("result: %+v", res)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		file      string
		retry     string
		transient bool
		want      string
		after     time.Duration
	}{
		{"bad key", 401, "error_unauthorized.json", "", false, "rejected the API key", 0},
		{"credits", 402, "error_insufficient_credits.json", "", false, "insufficient credits", 0},
		{"in-flight budget", 402, "error_insufficient_credits.json", "7", true, "spending limit", 7 * time.Second},
		{"moderation", 403, "error_moderation.json", "", false, "content moderation (sexual)", 0},
		{"rate limit", 429, "error_rate_limit.json", "", true, "rate limit", 10 * time.Second},
		{"rate limit with retry-after", 429, "error_rate_limit.json", "3", true, "rate limit", 3 * time.Second},
		{"upstream", 502, "error_upstream.json", "", true, "failed upstream", 0},
		{"bad request", 400, "error_bad_request.json", "", false, "aspect_ratio must be one of", 0},
		{"error inside 200", 200, "error_in_200.json", "", true, "Provider returned error", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := newFakeAPI(t)
			api.respond(c.status, c.file)
			if c.retry != "" {
				api.header.Set("Retry-After", c.retry)
			}
			p := api.provider(t, nil)
			_, err := p.Submit(context.Background(), referenceRequest(t))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error: %v (want %q)", err, c.want)
			}
			transient, after := provider.IsTransient(err)
			if transient != c.transient || after != c.after {
				t.Fatalf("transient=%v after=%s, want %v %s", transient, after, c.transient, c.after)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Fatal("the error leaks the API key")
			}
		})
	}
}

func TestUnreachableIsTransient(t *testing.T) {
	p, err := New(Config{APIKey: testKey, BaseURL: "http://127.0.0.1:1/api/v1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Submit(context.Background(), referenceRequest(t))
	if ok, _ := provider.IsTransient(err); !ok {
		t.Fatalf("want transient, got %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("the error leaks the API key")
	}
}

func TestStaleResultsAreDropped(t *testing.T) {
	api := newFakeAPI(t)
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	p := api.provider(t, func(c *Config) { c.Now = func() time.Time { return now } })
	old, err := p.Submit(context.Background(), referenceRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * resultTTL)
	if _, err := p.Submit(context.Background(), referenceRequest(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Poll(context.Background(), old); !errors.Is(err, provider.ErrUnknownJob) {
		t.Fatalf("stale result: %v", err)
	}
}

func TestConfig(t *testing.T) {
	for name, cfg := range map[string]Config{
		"no key":     {},
		"bare model": {APIKey: "k", Model: "gemini-3-pro-image"},
		"resolution": {APIKey: "k", Resolution: "8K"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	p, err := New(Config{APIKey: "k", Resolution: "4k"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Model(provider.ReferenceGenerate) != DefaultModel+" (4K)" || p.Name() != Name {
		t.Fatalf("model: %s", p.Model(provider.ReferenceGenerate))
	}
	if !provider.Supports(p, provider.ReferenceGenerate) || !provider.Supports(p, provider.Edit) || provider.Supports(p, provider.TryOn) {
		t.Fatal("capabilities")
	}
}

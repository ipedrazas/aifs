package fashn

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/provider"
)

const testKey = "test-key-123"

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
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	runs     []map[string]any
	run      func(w http.ResponseWriter)
	statuses []string // fixtures returned by successive status calls
	credits  string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{t: t, credits: "3"}
	f.run = func(w http.ResponseWriter) { _, _ = w.Write(fixture(t, "run_ok.json")) }
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fixture(f.t, "output.png"))
		return
	case r.Header.Get("Authorization") != "Bearer "+testKey:
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(fixture(f.t, "error_unauthorized.json"))
		return
	case r.Method == http.MethodPost && r.URL.Path == "/v1/run":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.runs = append(f.runs, body)
		f.mu.Unlock()
		f.run(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/status/"):
		f.mu.Lock()
		if len(f.statuses) == 0 {
			f.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(fixture(f.t, "error_not_found.json"))
			return
		}
		name := f.statuses[0]
		f.statuses = f.statuses[1:]
		f.mu.Unlock()
		body := strings.ReplaceAll(string(fixture(f.t, name)), "{{CDN}}", f.srv.URL+"/cdn")
		if strings.Contains(name, "completed") {
			w.Header().Set("x-fashn-credits-used", f.credits)
		}
		_, _ = w.Write([]byte(body))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) provider(t *testing.T, cfg Config) *Provider {
	t.Helper()
	u, _ := url.Parse(f.srv.URL)
	if cfg.APIKey == "" {
		cfg.APIKey = testKey
	}
	cfg.BaseURL = f.srv.URL
	cfg.OutputHosts = []string{u.Hostname()}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func tryOnRequest(t *testing.T) provider.Request {
	return provider.Request{
		Capability:      provider.TryOn,
		Prompt:          "hands in pockets",
		Source:          &provider.Image{Role: "source", ContentType: "image/jpeg", Data: imageFixture(t, "rot6.jpg")},
		Garments:        []provider.Image{{Role: "garment", Label: "front", ContentType: "image/png", Data: imageFixture(t, "plain.png")}},
		Seed:            (1 << 40) + 7,
		GarmentCategory: "jeans",
	}
}

func TestSubmitTryOnMax(t *testing.T) {
	api := newFakeAPI(t)
	p := api.provider(t, Config{Resolution: "2k", Mode: "quality"})
	id, err := p.Submit(context.Background(), tryOnRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if id != "123a87r9-4129-4bb3-be18-9c9fb5bd7fc1-u1" {
		t.Fatalf("id %q", id)
	}
	body := api.runs[0]
	in := body["inputs"].(map[string]any)
	if body["model_name"] != "tryon-max" || in["resolution"] != "2k" || in["generation_mode"] != "quality" ||
		in["prompt"] != "hands in pockets" || in["num_images"] != float64(1) || in["return_base64"] != false {
		t.Fatalf("body: %v", body)
	}
	if in["seed"] != float64(7) {
		t.Fatalf("seed not reduced to 32 bits: %v", in["seed"])
	}
	// Images are sent unmodified, as data URIs.
	garment := in["product_image"].(string)
	if !strings.HasPrefix(garment, "data:image/png;base64,") {
		t.Fatalf("garment: %.40s", garment)
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(garment, "data:image/png;base64,"))
	if !bytes.Equal(raw, imageFixture(t, "plain.png")) {
		t.Fatal("garment bytes changed")
	}
	if !strings.HasPrefix(in["model_image"].(string), "data:image/jpeg;base64,") {
		t.Fatal("model image is not a JPEG data URI")
	}
	if _, ok := in["garment_image"]; ok {
		t.Fatal("v1.6 field sent to tryon-max")
	}
}

func TestSubmitTryOnV16(t *testing.T) {
	api := newFakeAPI(t)
	p := api.provider(t, Config{Model: ModelTryOnV16, Mode: "fast"})
	if _, err := p.Submit(context.Background(), tryOnRequest(t)); err != nil {
		t.Fatal(err)
	}
	in := api.runs[0]["inputs"].(map[string]any)
	if api.runs[0]["model_name"] != "tryon-v1.6" || in["category"] != "bottoms" || in["mode"] != "performance" ||
		in["num_samples"] != float64(1) || in["garment_image"] == nil {
		t.Fatalf("inputs: %v", in)
	}
	if _, ok := in["prompt"]; ok {
		t.Fatal("v1.6 takes no prompt")
	}
}

func TestPollLifecycle(t *testing.T) {
	ctx := context.Background()
	api := newFakeAPI(t)
	api.statuses = []string{"status_in_queue.json", "status_processing.json", "status_completed.json"}
	p := api.provider(t, Config{})

	for i := 0; i < 2; i++ {
		res, err := p.Poll(ctx, "123a87r9-4129-4bb3-be18-9c9fb5bd7fc1-u1")
		if err != nil || res.Status != provider.Pending {
			t.Fatalf("poll %d: %+v %v", i, res, err)
		}
	}
	res, err := p.Poll(ctx, "123a87r9-4129-4bb3-be18-9c9fb5bd7fc1-u1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != provider.Succeeded || len(res.Images) != 1 || !bytes.Equal(res.Images[0].Data, fixture(t, "output.png")) {
		t.Fatalf("completed: %+v", res.Status)
	}
	if !res.CostReported || res.CostMicros != 3*75_000 {
		t.Fatalf("cost: %+v", res)
	}
}

func TestPollFailuresAreReadable(t *testing.T) {
	ctx := context.Background()
	for fixtureName, want := range map[string]string{
		"status_failed_pose.json":  "pose could not be detected",
		"status_failed_image.json": "an input image could not be read",
	} {
		api := newFakeAPI(t)
		api.statuses = []string{fixtureName}
		res, err := api.provider(t, Config{}).Poll(ctx, "x")
		if err != nil || res.Status != provider.Failed || !strings.Contains(res.Error, want) {
			t.Errorf("%s: %+v %v", fixtureName, res, err)
		}
	}

	// An id FASHN does not know lets the runner resubmit.
	api := newFakeAPI(t)
	if _, err := api.provider(t, Config{}).Poll(ctx, "gone"); !errors.Is(err, provider.ErrUnknownJob) {
		t.Fatalf("unknown job: %v", err)
	}
}

func TestHTTPErrorsAreClassified(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		fixture   string
		transient bool
		msg       string
	}{
		{"bad key", 401, "error_unauthorized.json", false, "rejected the API key"},
		{"rate limit", 429, "error_rate_limit.json", true, "rate limit"},
		{"concurrency", 429, "error_concurrency.json", true, "ConcurrencyLimitExceeded"},
		{"credits", 429, "error_out_of_credits.json", false, "no credits left"},
		{"bad request", 400, "error_bad_request.json", false, "product_image is required"},
		{"server", 500, "", true, "server error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := newFakeAPI(t)
			api.run = func(w http.ResponseWriter) {
				if c.status == 429 && c.transient {
					w.Header().Set("Retry-After", "7")
				}
				w.WriteHeader(c.status)
				if c.fixture != "" {
					_, _ = w.Write(fixture(t, c.fixture))
				}
			}
			cfg := Config{}
			if c.name == "bad key" {
				cfg.APIKey = "wrong"
			}
			_, err := api.provider(t, cfg).Submit(context.Background(), tryOnRequest(t))
			if err == nil || !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("error: %v", err)
			}
			transient, after := provider.IsTransient(err)
			if transient != c.transient {
				t.Fatalf("transient = %v", transient)
			}
			if c.name == "rate limit" && after != 7*time.Second {
				t.Fatalf("retry after %s", after)
			}
			if strings.Contains(err.Error(), testKey) || strings.Contains(err.Error(), "wrong") {
				t.Fatal("the API key leaked into an error")
			}
		})
	}
}

func TestUnreachableAPIIsTransientAndHidesKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	p, _ := New(Config{APIKey: testKey, BaseURL: base})
	_, err := p.Submit(context.Background(), tryOnRequest(t))
	if ok, _ := provider.IsTransient(err); !ok {
		t.Fatalf("unreachable: %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("key in error")
	}
}

func TestDownloadOnlyFromAllowedHosts(t *testing.T) {
	api := newFakeAPI(t)
	api.statuses = []string{"status_completed.json"}
	p := api.provider(t, Config{})
	p.cfg.OutputHosts = []string{"cdn.fashn.ai"}
	if _, err := p.Poll(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("download from an unexpected host: %v", err)
	}
}

func TestValidationAndPricing(t *testing.T) {
	for _, cfg := range []Config{{}, {APIKey: "k", Model: "tryon-v9"}, {APIKey: "k", Resolution: "8k"}, {APIKey: "k", Mode: "turbo"}} {
		if _, err := New(cfg); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
	for _, c := range []struct {
		cfg     Config
		credits int64
	}{
		{Config{APIKey: "k"}, 3}, // 2k balanced
		{Config{APIKey: "k", Resolution: "1k", Mode: "fast"}, 1},
		{Config{APIKey: "k", Resolution: "4k", Mode: "quality"}, 5},
		{Config{APIKey: "k", Model: ModelTryOnV16, Mode: "quality"}, 1},
	} {
		p, _ := New(c.cfg)
		if p.CreditsPerCall() != c.credits || p.UnitPriceMicros() != c.credits*75_000 {
			t.Errorf("%+v: %d credits", c.cfg, p.CreditsPerCall())
		}
	}

	p, _ := New(Config{APIKey: "k"})
	req := tryOnRequest(t)
	req.Source = nil
	if _, err := p.Submit(context.Background(), req); err == nil {
		t.Fatal("try-on without a person image")
	}
	req = tryOnRequest(t)
	req.Capability = provider.ReferenceGenerate
	if _, err := p.Submit(context.Background(), req); err == nil {
		t.Fatal("unsupported capability")
	}
	if p.Model(provider.TryOn) != "tryon-max (2k, balanced)" {
		t.Fatalf("model: %s", p.Model(provider.TryOn))
	}
}

package replicate

import (
	"bytes"
	"context"
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

const testToken = "r8_test_token"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
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
	created  []map[string]any
	headers  []http.Header
	create   func(w http.ResponseWriter)
	statuses []string // fixtures returned by successive GETs
	cdnAuth  string   // Authorization header seen by the CDN
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{t: t}
	f.create = func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(fixture(t, "prediction_starting.json"))
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		f.mu.Lock()
		f.cdnAuth = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fixture(f.t, "mask.png"))
	case r.Header.Get("Authorization") != "Bearer "+testToken:
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthenticated","detail":"You did not pass a valid authentication token","status":401}`))
	case r.Method == http.MethodPost && r.URL.Path == "/v1/predictions":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.created = append(f.created, body)
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		f.create(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/predictions/"):
		f.mu.Lock()
		if len(f.statuses) == 0 {
			f.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"Not found."}`))
			return
		}
		name := f.statuses[0]
		f.statuses = f.statuses[1:]
		f.mu.Unlock()
		body := strings.ReplaceAll(string(fixture(f.t, name)), "{{CDN}}", f.srv.URL+"/cdn")
		_, _ = w.Write([]byte(body))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeAPI) provider(t *testing.T) *Provider {
	u, _ := url.Parse(f.srv.URL)
	p, err := New(Config{APIToken: testToken, BaseURL: f.srv.URL, OutputHosts: []string{u.Hostname()}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func request() provider.Request {
	return provider.Request{
		Capability: provider.RemoveBackground,
		Source:     &provider.Image{Role: "source", ContentType: "image/jpeg", Data: []byte("\xff\xd8\xff fake jpeg")},
	}
}

func TestRemoveBackground(t *testing.T) {
	f := newFakeAPI(t)
	p := f.provider(t)
	ctx := context.Background()

	id, err := p.Submit(ctx, request())
	if err != nil || id != "nrj1aa7kz5rg80d1x3g8nfasj4" {
		t.Fatalf("submit: %q %v", id, err)
	}
	body := f.created[0]
	if body["version"] != "a029dff38972b5fda4ec5d75d7d1cd25aeff621d2cf4946a41055d7db66b80bc" {
		t.Fatalf("version: %v", body["version"])
	}
	in := body["input"].(map[string]any)
	if in["background_type"] != "map" || !strings.HasPrefix(in["image"].(string), "data:image/jpeg;base64,") {
		t.Fatalf("input: %v", in)
	}
	if f.headers[0].Get("Cancel-After") == "" {
		t.Fatal("a prediction must not run forever")
	}

	f.statuses = []string{"prediction_starting.json", "prediction_succeeded.json"}
	if res, err := p.Poll(ctx, id); err != nil || res.Status != provider.Pending {
		t.Fatalf("pending: %+v %v", res, err)
	}
	res, err := p.Poll(ctx, id)
	if err != nil || res.Status != provider.Succeeded || len(res.Images) != 1 {
		t.Fatalf("succeeded: %+v %v", res, err)
	}
	if !bytes.Equal(res.Images[0].Data, fixture(t, "mask.png")) || res.Images[0].ContentType != "image/png" {
		t.Fatal("mask not downloaded")
	}
	if res.CostReported {
		t.Fatal("Replicate reports no cost; the unit price applies")
	}
	if f.cdnAuth != "" {
		t.Fatal("the API token must not be sent to the delivery host")
	}
	if p.Model(provider.RemoveBackground) != DefaultModel {
		t.Fatal(p.Model(provider.RemoveBackground))
	}
}

func TestFailedPrediction(t *testing.T) {
	f := newFakeAPI(t)
	p := f.provider(t)
	f.statuses = []string{"prediction_failed.json"}
	res, err := p.Poll(context.Background(), "4q52jj2s9drge0d1x3gbthth80")
	if err != nil || res.Status != provider.Failed || !strings.Contains(res.Error, "cannot identify image file") {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := p.Poll(context.Background(), "gone"); !errors.Is(err, provider.ErrUnknownJob) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestErrors(t *testing.T) {
	for name, c := range map[string]struct {
		status    int
		header    string
		body      string
		transient bool
		retry     time.Duration
		contains  string
	}{
		"rate limit":   {429, "", `{"detail":"Request was throttled. Expected available in 1 second."}`, true, 10 * time.Second, "rate limit"},
		"bad gateway":  {502, "60", `<html>`, true, 60 * time.Second, "HTTP 502"},
		"no credit":    {402, "", `{"title":"Insufficient credit"}`, false, 0, "no credit"},
		"invalid":      {422, "", `{"title":"Input validation failed","detail":"- input.image: Does not match format 'uri'"}`, false, 0, "Does not match format"},
		"unknown body": {400, "", ``, false, 0, "HTTP 400"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeAPI(t)
			f.create = func(w http.ResponseWriter) {
				if c.header != "" {
					w.Header().Set("Retry-After", c.header)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}
			_, err := f.provider(t).Submit(context.Background(), request())
			if err == nil || !strings.Contains(err.Error(), c.contains) {
				t.Fatalf("error: %v", err)
			}
			transient, retry := provider.IsTransient(err)
			if transient != c.transient || retry != c.retry {
				t.Fatalf("transient=%v retry=%v", transient, retry)
			}
		})
	}

	f := newFakeAPI(t)
	p, _ := New(Config{APIToken: "wrong", BaseURL: f.srv.URL})
	_, err := p.Submit(context.Background(), request())
	if err == nil || !strings.Contains(err.Error(), "AIFS_REPLICATE_API_TOKEN") || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("bad token: %v", err)
	}
}

func TestRefusesForeignDownloads(t *testing.T) {
	f := newFakeAPI(t)
	p, _ := New(Config{APIToken: testToken, BaseURL: f.srv.URL}) // test host not allowed
	f.statuses = []string{"prediction_succeeded.json"}
	if _, err := p.Poll(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("got %v", err)
	}
	for h, ok := range map[string]bool{"replicate.delivery": true, "pbxt.replicate.delivery": true, "evilreplicate.delivery": false, "replicate.delivery.evil.com": false} {
		if p.allowedHost(h) != ok {
			t.Errorf("%s: %v", h, !ok)
		}
	}
}

func TestConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("token required")
	}
	for _, m := range []string{"no-version", "owner/name:", ":abc"} {
		if _, err := New(Config{APIToken: "x", Model: m}); err == nil {
			t.Errorf("%q accepted", m)
		}
	}
	p, _ := New(Config{APIToken: "x"})
	if _, err := p.Submit(context.Background(), provider.Request{Capability: provider.TryOn}); err == nil {
		t.Fatal("unsupported capability accepted")
	}
}

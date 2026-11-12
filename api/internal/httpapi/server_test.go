package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

type fakeDB struct {
	pingErr error
	schema  int
}

func (f fakeDB) Ping(context.Context) error                 { return f.pingErr }
func (f fakeDB) SchemaVersion(context.Context) (int, error) { return f.schema, nil }

var webFS = fstest.MapFS{
	"index.html":        {Data: []byte(`<!doctype html><div id="root"></div>`)},
	"assets/app-abc.js": {Data: []byte(`console.log(1)`)},
	"favicon.svg":       {Data: []byte(`<svg/>`)},
}

func newServer(db fakeDB, web fstest.MapFS) http.Handler {
	return New(Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:     db,
		Web:    web,
	})
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := do(t, newServer(fakeDB{}, webFS), http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}

	rec = do(t, newServer(fakeDB{pingErr: errors.New("down")}, webFS), http.MethodGet, "/healthz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
}

func TestVersion(t *testing.T) {
	rec := do(t, newServer(fakeDB{schema: 3}, webFS), http.MethodGet, "/api/v1/version")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
	var v VersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Version == "" || v.SchemaVersion != 3 {
		t.Fatalf("unexpected body %+v", v)
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	h := newServer(fakeDB{}, webFS)
	for _, p := range []string{"/api/v1/nope", "/api/other"} {
		rec := do(t, h, http.MethodGet, p)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d", p, rec.Code)
		}
		if p == "/api/v1/nope" && !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Fatalf("%s: body %s", p, rec.Body)
		}
	}
}

func TestSPA(t *testing.T) {
	h := newServer(fakeDB{}, webFS)

	cases := []struct {
		path   string
		status int
		body   string
		cache  string
	}{
		{"/", 200, `id="root"`, "no-cache"},
		{"/products/42", 200, `id="root"`, "no-cache"},
		{"/assets/app-abc.js", 200, "console.log", "public, max-age=31536000, immutable"},
		{"/favicon.svg", 200, "<svg/>", ""},
		{"/assets/missing.js", 404, "", ""},
	}
	for _, c := range cases {
		rec := do(t, h, http.MethodGet, c.path)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
			continue
		}
		if c.body != "" && !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: body %q", c.path, rec.Body)
		}
		if c.cache != "" && rec.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s: cache-control %q", c.path, rec.Header().Get("Cache-Control"))
		}
	}

	if rec := do(t, h, http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /: got %d", rec.Code)
	}
}

func TestSPANotBuilt(t *testing.T) {
	rec := do(t, newServer(fakeDB{}, fstest.MapFS{}), http.MethodGet, "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "not built") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := do(t, newServer(fakeDB{}, webFS), http.MethodGet, "/")
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}

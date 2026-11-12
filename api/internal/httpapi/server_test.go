package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ipedrazas/aifs/api/internal/auth"
	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/db"
	"github.com/ipedrazas/aifs/api/internal/media"
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

const testPassword = "correct horse battery"

func newServerT(t *testing.T, fdb fakeDB, web fstest.MapFS) http.Handler {
	t.Helper()
	return newTestServer(t, fdb, web, 1<<20)
}

func newTestServer(t *testing.T, fdb fakeDB, web fstest.MapFS, maxUpload int64) http.Handler {
	t.Helper()
	h, _ := newTestServerDB(t, fdb, web, maxUpload)
	return h
}

// newTestServerDB also returns the database, for tests that need to set up
// state the API cannot create yet.
func newTestServerDB(t *testing.T, fdb fakeDB, web fstest.MapFS, maxUpload int64) (http.Handler, *db.DB) {
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
	return New(Deps{
		Logger:         log,
		DB:             fdb,
		Web:            web,
		Auth:           newAuth(t, d),
		CookieSecure:   true,
		Catalog:        catalog.New(d, store, log),
		Media:          media.New(d, store),
		MaxUploadBytes: maxUpload,
	}), d
}

func newAuth(t *testing.T, d *db.DB) *auth.Service {
	t.Helper()
	a, err := auth.New(context.Background(), d, auth.Options{
		Password: testPassword,
		TTL:      time.Hour,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),

		FingerprintIterations: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func do(t *testing.T, h http.Handler, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return doBody(t, h, method, target, "", cookies...)
}

func doBody(t *testing.T, h http.Handler, method, target, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.RemoteAddr = "192.0.2.1:1234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// login returns the session cookie for a successful login.
func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	rec := doBody(t, h, http.MethodPost, "/api/v1/session", `{"password":"`+testPassword+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-aifs_session" {
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func TestHealthz(t *testing.T) {
	rec := do(t, newServerT(t, fakeDB{}, webFS), http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}

	rec = do(t, newServerT(t, fakeDB{pingErr: errors.New("down")}, webFS), http.MethodGet, "/healthz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
}

func TestVersion(t *testing.T) {
	h := newServerT(t, fakeDB{schema: 3}, webFS)
	rec := do(t, h, http.MethodGet, "/api/v1/version", login(t, h))
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
	h := newServerT(t, fakeDB{}, webFS)
	cookie := login(t, h)
	for _, p := range []string{"/api/v1/nope", "/api/other"} {
		rec := do(t, h, http.MethodGet, p, cookie)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d", p, rec.Code)
		}
		if p == "/api/v1/nope" && !strings.Contains(rec.Body.String(), `"not_found"`) {
			t.Fatalf("%s: body %s", p, rec.Body)
		}
	}
}

func TestSPA(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)

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
	rec := do(t, newServerT(t, fakeDB{}, fstest.MapFS{}), http.MethodGet, "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "not built") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := do(t, newServerT(t, fakeDB{}, webFS), http.MethodGet, "/")
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}

func newReq(method, target, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.RemoteAddr = "192.0.2.1:1234"
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

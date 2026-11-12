package httpapi

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/catalog"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "imaging", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return v
}

func uploadProduct(t *testing.T, h http.Handler, cookie *http.Cookie, productID int64, view, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	return uploadTo(t, h, cookie, "/api/v1/products/"+strconv.FormatInt(productID, 10)+"/images", "view", view, filename, data)
}

func uploadTo(t *testing.T, h http.Handler, cookie *http.Cookie, path, labelField, label, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if label != "" {
		_ = mw.WriteField(labelField, label)
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(data)
	_ = mw.Close()

	req := newReq(http.MethodPost, path, "")
	req.Body = httpBody(body.Bytes())
	req.ContentLength = int64(body.Len())
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	return serve(h, req)
}

func httpBody(b []byte) *readCloser { return &readCloser{bytes.NewReader(b)} }

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }

func createProduct(t *testing.T, h http.Handler, c *http.Cookie, body string) catalog.Product {
	t.Helper()
	rec := doBody(t, h, http.MethodPost, "/api/v1/products", body, c)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	return decode[catalog.Product](t, rec)
}

func TestProductCRUD(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	c := login(t, h)

	p := createProduct(t, h, c, `{"name":"Wide-leg jeans","sku":"WL-7","colour":"black","printDetails":"logo on hip","notes":"n","description":"d"}`)
	if p.ID == 0 || p.Category != "jeans" || p.PrintDetails != "logo on hip" {
		t.Fatalf("created: %+v", p)
	}
	id := strconv.FormatInt(p.ID, 10)

	rec := doBody(t, h, http.MethodPatch, "/api/v1/products/"+id, `{"colour":"washed black"}`, c)
	if rec.Code != http.StatusOK || decode[catalog.Product](t, rec).Colour != "washed black" {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}

	rec = do(t, h, http.MethodGet, "/api/v1/products", c)
	list := decode[ProductList](t, rec)
	if len(list.Items) != 1 || list.Items[0].Name != "Wide-leg jeans" {
		t.Fatalf("list: %s", rec.Body)
	}

	rec = doBody(t, h, http.MethodPost, "/api/v1/products", `{"name":"other","sku":"wl-7"}`, c)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate sku: %d", rec.Code)
	}
	rec = doBody(t, h, http.MethodPost, "/api/v1/products", `{"sku":"x"}`, c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"name"`) {
		t.Fatalf("missing name: %d %s", rec.Code, rec.Body)
	}
	rec = doBody(t, h, http.MethodPost, "/api/v1/products", `{"name":"x","price":3}`, c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}

	if rec := do(t, h, http.MethodDelete, "/api/v1/products/"+id, c); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	for _, path := range []string{"/api/v1/products/" + id, "/api/v1/products/abc", "/api/v1/products/0"} {
		if rec := do(t, h, http.MethodGet, path, c); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestUploadAndDownloadByteIdentical(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	c := login(t, h)
	p := createProduct(t, h, c, `{"name":"jeans"}`)

	for _, f := range []struct{ file, view, ct string }{
		{"rot6.jpg", "front", "image/jpeg"},
		{"plain.png", "back", "image/png"},
		{"plain.webp", "print", "image/webp"},
	} {
		data := fixture(t, f.file)
		rec := uploadProduct(t, h, c, p.ID, f.view, f.file, data)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: %d %s", f.file, rec.Code, rec.Body)
		}
		im := decode[catalog.ProductImage](t, rec)
		if im.View != f.view || im.ContentType != f.ct || im.Filename != f.file {
			t.Fatalf("%s: %+v", f.file, im)
		}

		rec = do(t, h, http.MethodGet, "/api/v1/blobs/"+im.Hash, c)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), data) {
			t.Fatalf("%s: download differs (%d)", f.file, rec.Code)
		}
		if rec.Header().Get("Content-Type") != f.ct || rec.Header().Get("ETag") == "" {
			t.Fatalf("%s: headers %v", f.file, rec.Header())
		}

		// Conditional request.
		req := newReq(http.MethodGet, "/api/v1/blobs/"+im.Hash, "")
		req.AddCookie(c)
		req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
		if r := serve(h, req); r.Code != http.StatusNotModified {
			t.Fatalf("%s: If-None-Match: %d", f.file, r.Code)
		}
	}

	got := decode[catalog.Product](t, do(t, h, http.MethodGet, "/api/v1/products/"+strconv.FormatInt(p.ID, 10), c))
	if got.ImageCount != 3 || len(got.Images) != 3 || got.Images[0].View != "front" {
		t.Fatalf("product: %+v", got)
	}
}

func TestThumbnail(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	c := login(t, h)
	p := createProduct(t, h, c, `{"name":"jeans"}`)
	im := decode[catalog.ProductImage](t, uploadProduct(t, h, c, p.ID, "front", "r.jpg", fixture(t, "rot6.jpg")))

	rec := do(t, h, http.MethodGet, "/api/v1/blobs/"+im.Hash+"?w=300", c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb: %d %v", rec.Code, rec.Header())
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil || cfg.Width != 20 || cfg.Height != 40 {
		t.Fatalf("thumbnail not upright: %+v %v", cfg, err)
	}
	if rec.Header().Get("ETag") != `"`+im.Hash+`-w512"` {
		t.Fatalf("etag %q", rec.Header().Get("ETag"))
	}

	// The original is unchanged by thumbnailing.
	orig := do(t, h, http.MethodGet, "/api/v1/blobs/"+im.Hash, c)
	if !bytes.Equal(orig.Body.Bytes(), fixture(t, "rot6.jpg")) {
		t.Fatal("original changed")
	}

	for _, q := range []string{"?w=0", "?w=x", "?w=-5"} {
		if rec := do(t, h, http.MethodGet, "/api/v1/blobs/"+im.Hash+q, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
	for _, hash := range []string{strings.Repeat("0", 64), "nothex", "..%2F..%2Fetc"} {
		if rec := do(t, h, http.MethodGet, "/api/v1/blobs/"+hash, c); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", hash, rec.Code)
		}
	}
}

func TestUploadRejections(t *testing.T) {
	h := newTestServer(t, fakeDB{}, webFS, 400) // tiny limit: 400 bytes
	c := login(t, h)
	p := createProduct(t, h, c, `{"name":"jeans"}`)

	cases := []struct {
		name, view, file string
		data             []byte
		status           int
		msg              string
	}{
		{"gif", "front", "a.gif", fixture(t, "plain.gif"), 422, "only JPEG, PNG and WebP"},
		{"text", "front", "a.jpg", fixture(t, "text.jpg"), 422, "only JPEG, PNG and WebP"},
		{"corrupt", "front", "a.jpg", fixture(t, "truncated.jpg"), 422, "damaged or incomplete"},
		{"oversized", "front", "big.jpg", fixture(t, "rot6.jpg"), 413, "larger than"},
		{"no view", "", "a.jpg", fixture(t, "plain.jpg"), 422, "view"},
		{"bad view", "top", "a.jpg", fixture(t, "plain.jpg"), 422, "view"},
	}
	for _, tc := range cases {
		rec := uploadProduct(t, h, c, p.ID, tc.view, tc.file, tc.data)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.msg) {
			t.Errorf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}

	if rec := uploadProduct(t, h, c, 9999, "front", "a.jpg", fixture(t, "plain.jpg")); rec.Code != http.StatusNotFound {
		t.Errorf("missing product: %d", rec.Code)
	}

	req := newReq(http.MethodPost, "/api/v1/products/"+strconv.FormatInt(p.ID, 10)+"/images", `{"not":"multipart"}`)
	req.AddCookie(c)
	if rec := serve(h, req); rec.Code != http.StatusBadRequest {
		t.Errorf("non-multipart: %d", rec.Code)
	}
}

func TestOriginalsCannotBeModified(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	c := login(t, h)
	p := createProduct(t, h, c, `{"name":"jeans"}`)
	im := decode[catalog.ProductImage](t, uploadProduct(t, h, c, p.ID, "front", "a.jpg", fixture(t, "plain.jpg")))
	base := "/api/v1/products/" + strconv.FormatInt(p.ID, 10) + "/images/" + strconv.FormatInt(im.ID, 10)

	// No route modifies an image record or its bytes.
	for _, m := range []string{http.MethodPut, http.MethodPatch, http.MethodPost} {
		if rec := doBody(t, h, m, base, `{"view":"back"}`, c); rec.Code < 400 {
			t.Errorf("%s image: %d", m, rec.Code)
		}
	}
	for _, m := range []string{http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch} {
		if rec := doBody(t, h, m, "/api/v1/blobs/"+im.Hash, "x", c); rec.Code < 400 {
			t.Errorf("%s blob: %d", m, rec.Code)
		}
	}
	got := decode[catalog.Product](t, do(t, h, http.MethodGet, "/api/v1/products/"+strconv.FormatInt(p.ID, 10), c))
	if len(got.Images) != 1 || got.Images[0].View != "front" || got.Images[0].Hash != im.Hash {
		t.Fatalf("image changed: %+v", got.Images)
	}
	if rec := do(t, h, http.MethodGet, "/api/v1/blobs/"+im.Hash, c); !bytes.Equal(rec.Body.Bytes(), fixture(t, "plain.jpg")) {
		t.Fatal("bytes changed")
	}

	if rec := do(t, h, http.MethodDelete, base, c); rec.Code != http.StatusNoContent {
		t.Fatalf("delete image: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/api/v1/blobs/"+im.Hash, c); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted blob still served: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, base, c); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete: %d", rec.Code)
	}
}

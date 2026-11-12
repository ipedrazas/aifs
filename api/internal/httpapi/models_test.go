package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ipedrazas/aifs/api/internal/catalog"
)

func uploadModel(t *testing.T, h http.Handler, c *http.Cookie, modelID int64, role, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	return uploadTo(t, h, c, "/api/v1/models/"+strconv.FormatInt(modelID, 10)+"/images", "role", role, filename, data)
}

func createModel(t *testing.T, h http.Handler, c *http.Cookie, body string) catalog.Model {
	t.Helper()
	rec := doBody(t, h, http.MethodPost, "/api/v1/models", body, c)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create model: %d %s", rec.Code, rec.Body)
	}
	return decode[catalog.Model](t, rec)
}

func TestModelCRUDAndImages(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	c := login(t, h)

	m := createModel(t, h, c, `{"name":"Ava","description":"relaxed","appearance":"tall, dark hair","preferredPoses":"walking","styling":"white trainers"}`)
	if m.Origin != "uploaded" || m.Archived || m.Appearance != "tall, dark hair" {
		t.Fatalf("created: %+v", m)
	}
	id := strconv.FormatInt(m.ID, 10)

	rec := doBody(t, h, http.MethodPatch, "/api/v1/models/"+id, `{"styling":"boots"}`, c)
	if rec.Code != http.StatusOK || decode[catalog.Model](t, rec).Styling != "boots" {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	if rec := doBody(t, h, http.MethodPost, "/api/v1/models", `{"description":"no name"}`, c); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing name: %d", rec.Code)
	}

	data := fixture(t, "rot6.jpg")
	rec = uploadModel(t, h, c, m.ID, "identity", "face.jpg", data)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	im := decode[catalog.ModelImage](t, rec)
	if im.Role != "identity" || im.ModelID != m.ID {
		t.Fatalf("image: %+v", im)
	}
	if rec := do(t, h, http.MethodGet, "/api/v1/blobs/"+im.Hash, c); !bytes.Equal(rec.Body.Bytes(), data) {
		t.Fatal("bytes differ")
	}

	for _, tc := range []struct{ role, file string }{{"headshot", "plain.jpg"}, {"", "plain.jpg"}, {"identity", "plain.gif"}} {
		if rec := uploadModel(t, h, c, m.ID, tc.role, "x", fixture(t, tc.file)); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%+v: %d %s", tc, rec.Code, rec.Body)
		}
	}

	got := decode[catalog.Model](t, do(t, h, http.MethodGet, "/api/v1/models/"+id, c))
	if got.ImageCount != 1 || got.Cover == nil || len(got.Images) != 1 {
		t.Fatalf("model: %+v", got)
	}

	imgPath := "/api/v1/models/" + id + "/images/" + strconv.FormatInt(im.ID, 10)
	if rec := do(t, h, http.MethodDelete, imgPath, c); rec.Code != http.StatusNoContent {
		t.Fatalf("delete image: %d", rec.Code)
	}

	if rec := do(t, h, http.MethodDelete, "/api/v1/models/"+id, c); rec.Code != http.StatusNoContent {
		t.Fatalf("delete unused model: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, http.MethodGet, "/api/v1/models/"+id, c); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted model: %d", rec.Code)
	}
}

func TestUsedModelIsArchived(t *testing.T) {
	h, d := newTestServerDB(t, fakeDB{}, webFS, 1<<20)
	c := login(t, h)
	m := createModel(t, h, c, `{"name":"Used"}`)
	id := strconv.FormatInt(m.ID, 10)

	// Stand-in for a generation that used the model (Phase 5).
	ctx := context.Background()
	if _, err := d.Write.ExecContext(ctx, `CREATE TABLE test_ref (model_id INTEGER REFERENCES model (id) ON DELETE RESTRICT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Write.ExecContext(ctx, `INSERT INTO test_ref VALUES (?)`, m.ID); err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodDelete, "/api/v1/models/"+id, c)
	if rec.Code != http.StatusOK || !decode[catalog.Model](t, rec).Archived {
		t.Fatalf("delete used model: %d %s", rec.Code, rec.Body)
	}

	list := decode[ModelList](t, do(t, h, http.MethodGet, "/api/v1/models", c))
	if len(list.Items) != 0 {
		t.Fatalf("archived model in default list: %+v", list)
	}
	list = decode[ModelList](t, do(t, h, http.MethodGet, "/api/v1/models?archived=true", c))
	if len(list.Items) != 1 || !list.Items[0].Archived {
		t.Fatalf("archived model missing: %+v", list)
	}

	rec = doBody(t, h, http.MethodPatch, "/api/v1/models/"+id, `{"archived":false}`, c)
	if rec.Code != http.StatusOK || decode[catalog.Model](t, rec).Archived {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(do(t, h, http.MethodGet, "/api/v1/models", c).Body.String(), `"Used"`) {
		t.Fatal("restored model not listed")
	}
}

package httpapi

import (
	"net/http"
	"strconv"

	"github.com/ipedrazas/aifs/api/internal/catalog"
)

type modelHandlers struct {
	d Deps
}

// ModelList is the body of GET /api/v1/models.
type ModelList struct {
	Items []catalog.Model `json:"items"`
}

func (h modelHandlers) list(w http.ResponseWriter, r *http.Request) {
	includeArchived, _ := strconv.ParseBool(r.URL.Query().Get("archived"))
	items, err := h.d.Catalog.ListModels(r.Context(), includeArchived)
	if err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ModelList{Items: items})
}

func (h modelHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in catalog.ModelInput
	if !decodeJSON(w, r, &in) {
		return
	}
	m, err := h.d.Catalog.CreateModel(r.Context(), in)
	if err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/models/"+strconv.FormatInt(m.ID, 10))
	writeJSON(w, http.StatusCreated, m)
}

func (h modelHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	m, err := h.d.Catalog.GetModel(r.Context(), id)
	if err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h modelHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in catalog.ModelInput
	if !decodeJSON(w, r, &in) {
		return
	}
	m, err := h.d.Catalog.UpdateModel(r.Context(), id, in)
	if err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// delete hard-deletes an unused model (204). A model that has been used is
// archived instead and returned (200), so its history stays traceable.
func (h modelHandlers) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	deleted, err := h.d.Catalog.DeleteModel(r.Context(), id)
	if err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	if deleted {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	m, err := h.d.Catalog.GetModel(r.Context(), id)
	if err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// uploadImage accepts multipart/form-data with a "role" field and one
// "file" part.
func (h modelHandlers) uploadImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	up, ok := readUpload(w, r, h.d.MaxUploadBytes, "role")
	if !ok {
		return
	}
	im, err := h.d.Catalog.AddModelImage(r.Context(), id, up.label, up.filename, up.data)
	if err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, im)
}

func (h modelHandlers) deleteImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	imageID, ok := pathID(w, r, "imageId")
	if !ok {
		return
	}
	if err := h.d.Catalog.DeleteModelImage(r.Context(), id, imageID); err != nil {
		failCatalog(h.d, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

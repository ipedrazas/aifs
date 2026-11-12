package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/imaging"
)

type productHandlers struct {
	d Deps
}

// ProductList is the body of GET /api/v1/products.
type ProductList struct {
	Items []catalog.Product `json:"items"`
}

func (h productHandlers) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.d.Catalog.ListProducts(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ProductList{Items: items})
}

func (h productHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in catalog.ProductInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := h.d.Catalog.CreateProduct(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/products/"+strconv.FormatInt(p.ID, 10))
	writeJSON(w, http.StatusCreated, p)
}

func (h productHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := h.d.Catalog.GetProduct(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h productHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in catalog.ProductInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := h.d.Catalog.UpdateProduct(r.Context(), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h productHandlers) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.d.Catalog.DeleteProduct(r.Context(), id); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// uploadImage accepts multipart/form-data with a "view" field and one
// "file" part.
func (h productHandlers) uploadImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	up, ok := readUpload(w, r, h.d.MaxUploadBytes, "view")
	if !ok {
		return
	}
	im, err := h.d.Catalog.AddProductImage(r.Context(), id, up.label, up.filename, up.data)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, im)
}

type upload struct {
	label    string // value of the label field (view or role)
	filename string
	data     []byte
}

// readUpload parses a multipart/form-data body with one text field named
// labelField and one "file" part. The file is read whole, bounded by limit;
// it is validated and stored byte for byte by the catalog.
func readUpload(w http.ResponseWriter, r *http.Request, limit int64, labelField string) (upload, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+64<<10) // headroom for the multipart envelope
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected a multipart/form-data upload")
		return upload{}, false
	}

	var up upload
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			uploadReadError(w, err, limit)
			return upload{}, false
		}
		switch part.FormName() {
		case labelField:
			b, err := io.ReadAll(io.LimitReader(part, 64))
			if err != nil {
				uploadReadError(w, err, limit)
				return upload{}, false
			}
			up.label = strings.TrimSpace(string(b))
		case "file":
			if up.data != nil {
				writeError(w, http.StatusBadRequest, "bad_request", "upload one file per request")
				return upload{}, false
			}
			up.filename = part.FileName()
			up.data, err = readPart(part, limit)
			if err != nil {
				uploadReadError(w, err, limit)
				return upload{}, false
			}
		}
		_ = part.Close()
	}
	if up.data == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "no file in the upload")
		return upload{}, false
	}
	return up, true
}

var errTooLarge = errors.New("too large")

func readPart(p *multipart.Part, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(p, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errTooLarge
	}
	return b, nil
}

func uploadReadError(w http.ResponseWriter, err error, limit int64) {
	var mbe *http.MaxBytesError
	if errors.Is(err, errTooLarge) || errors.As(err, &mbe) {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the file is larger than the "+formatMB(limit)+" limit")
		return
	}
	writeError(w, http.StatusBadRequest, "bad_request", "could not read the upload")
}

func (h productHandlers) deleteImage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	imageID, ok := pathID(w, r, "imageId")
	if !ok {
		return
	}
	if err := h.d.Catalog.DeleteProductImage(r.Context(), id, imageID); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func formatMB(n int64) string {
	if n >= 1<<20 {
		return strconv.FormatInt(n>>20, 10) + " MB"
	}
	return strconv.FormatInt(n, 10) + " byte"
}

func (h productHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	failCatalog(h.d, w, r, err)
}

// failCatalog maps catalog errors to HTTP responses.
func failCatalog(d Deps, w http.ResponseWriter, r *http.Request, err error) {
	var ve *catalog.ValidationError
	var ie *imaging.ValidationError
	var ce *catalog.ConflictError
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.As(err, &ve):
		writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: ErrorBody{
			Code: "invalid", Message: "some fields are invalid", Fields: ve.Fields,
		}})
	case errors.As(err, &ie):
		writeError(w, http.StatusUnprocessableEntity, "invalid_image", ie.Reason)
	case errors.As(err, &ce):
		writeError(w, http.StatusConflict, "conflict", ce.Reason)
	default:
		d.Logger.ErrorContext(r.Context(), "request failed", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	}
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return 0, false
	}
	return id, true
}

// decodeJSON reads a small JSON body, rejecting unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

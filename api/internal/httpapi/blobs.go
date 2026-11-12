package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ipedrazas/aifs/api/internal/blob"
	"github.com/ipedrazas/aifs/api/internal/media"
)

// blobHandler serves a registered blob. Without ?w= it returns the stored
// bytes unchanged; with ?w= an upright JPEG thumbnail at most that wide.
func blobHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := r.PathValue("hash")
		ctx := r.Context()

		var (
			obj      blob.Object
			meta     media.Meta
			etag, ct string
			err      error
		)
		if v := r.URL.Query().Get("w"); v != "" {
			width, perr := strconv.Atoi(v)
			if perr != nil || width <= 0 {
				writeError(w, http.StatusBadRequest, "bad_request", "w must be a positive integer")
				return
			}
			var snapped int
			obj, meta, snapped, err = d.Media.Thumbnail(ctx, hash, width)
			etag, ct = `"`+hash+"-w"+strconv.Itoa(snapped)+`"`, "image/jpeg"
		} else {
			obj, meta, err = d.Media.Open(ctx, hash)
			etag, ct = `"`+hash+`"`, meta.ContentType
		}

		if errors.Is(err, media.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such image")
			return
		}
		if err != nil {
			d.Logger.ErrorContext(ctx, "serve blob", "hash", hash, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not read the image")
			return
		}
		defer func() { _ = obj.Close() }()

		h := w.Header()
		h.Set("Content-Type", ct)
		h.Set("ETag", etag)
		// Content-addressed: the bytes behind a URL never change. Private
		// because every blob sits behind the session.
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		http.ServeContent(w, r, "", meta.CreatedAt, obj)
	}
}

// Package httpapi wires HTTP routes: the health check, the JSON API under
// /api/v1 and the embedded single-page application.
package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/ipedrazas/aifs/api/internal/auth"
	"github.com/ipedrazas/aifs/api/internal/catalog"
	"github.com/ipedrazas/aifs/api/internal/media"
	"github.com/ipedrazas/aifs/api/internal/version"
)

// Pinger reports whether a dependency (the database) is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// SchemaVersioner reports the applied database schema version.
type SchemaVersioner interface {
	SchemaVersion(ctx context.Context) (int, error)
}

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Logger *slog.Logger
	DB     interface {
		Pinger
		SchemaVersioner
	}
	// Web is the built frontend. It may be empty (no index.html).
	Web fs.FS
	// Auth checks the password and sessions.
	Auth *auth.Service
	// CookieSecure sets the Secure flag (and the __Host- prefix) on the
	// session cookie.
	CookieSecure bool
	// Catalog manages products and reference photographs.
	Catalog *catalog.Service
	// Media serves stored images and thumbnails.
	Media *media.Service
	// MaxUploadBytes bounds a single uploaded photograph.
	MaxUploadBytes int64
}

// New returns the root handler.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", health(d))

	sessions := newSessionHandlers(d)

	// Everything under /api/v1 requires a session, except logging in.
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/session", sessions.get)
	api.HandleFunc("DELETE /api/v1/session", sessions.logout)
	api.HandleFunc("GET /api/v1/version", versionInfo(d))

	products := productHandlers{d: d}
	api.HandleFunc("GET /api/v1/products", products.list)
	api.HandleFunc("POST /api/v1/products", products.create)
	api.HandleFunc("GET /api/v1/products/{id}", products.get)
	api.HandleFunc("PATCH /api/v1/products/{id}", products.update)
	api.HandleFunc("DELETE /api/v1/products/{id}", products.delete)
	api.HandleFunc("POST /api/v1/products/{id}/images", products.uploadImage)
	api.HandleFunc("DELETE /api/v1/products/{id}/images/{imageId}", products.deleteImage)

	models := modelHandlers{d: d}
	api.HandleFunc("GET /api/v1/models", models.list)
	api.HandleFunc("POST /api/v1/models", models.create)
	api.HandleFunc("GET /api/v1/models/{id}", models.get)
	api.HandleFunc("PATCH /api/v1/models/{id}", models.update)
	api.HandleFunc("DELETE /api/v1/models/{id}", models.delete)
	api.HandleFunc("POST /api/v1/models/{id}/images", models.uploadImage)
	api.HandleFunc("DELETE /api/v1/models/{id}/images/{imageId}", models.deleteImage)

	api.HandleFunc("GET /api/v1/blobs/{hash}", blobHandler(d))

	api.HandleFunc("/api/v1/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such API route")
	})

	mux.HandleFunc("POST /api/v1/session", sessions.login)
	mux.Handle("/api/", sessions.require(api))

	mux.Handle("/", spa(d.Web))

	csrf := http.NewCrossOriginProtection()
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected")
	}))

	return securityHeaders(requestLog(d.Logger, recoverer(d.Logger, csrf.Handler(mux))))
}

// HealthResponse is the body of GET /healthz.
type HealthResponse struct {
	Status string `json:"status"`
}

func health(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			d.Logger.ErrorContext(ctx, "health check failed", "err", err)
			writeJSON(w, http.StatusServiceUnavailable, HealthResponse{Status: "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
	}
}

// VersionResponse is the body of GET /api/v1/version.
type VersionResponse struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	SchemaVersion int    `json:"schemaVersion"`
}

func versionInfo(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sv, err := d.DB.SchemaVersion(r.Context())
		if err != nil {
			d.Logger.ErrorContext(r.Context(), "read schema version", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not read schema version")
			return
		}
		writeJSON(w, http.StatusOK, VersionResponse{
			Version:       version.Version,
			Commit:        version.Commit,
			SchemaVersion: sv,
		})
	}
}

// ErrorResponse is the body of every API error.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries a machine-readable code and a human-readable message.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Fields maps invalid input fields to a reason (validation errors only).
	Fields map[string]string `json:"fields,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, ErrorResponse{Error: ErrorBody{Code: code, Message: msg}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

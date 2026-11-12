package httpapi

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const notBuilt = `<!doctype html><meta charset="utf-8"><title>AI Fashion Studio</title>
<p>The frontend is not built into this binary. Run <code>task build</code>, or use <code>task dev</code> and open the Vite dev server.</p>`

// spa serves the built frontend. Existing files are served as-is (hashed
// assets get a long cache lifetime); any other GET path falls back to
// index.html so client-side routes work on reload.
func spa(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	index, indexErr := fs.ReadFile(web, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(web, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			// A missing file that looks like a static asset is a real 404,
			// not a client route.
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if indexErr != nil {
			_, _ = w.Write([]byte(notBuilt))
			return
		}
		_, _ = w.Write(index)
	})
}

// Package webui embeds the built React application.
//
// The dist directory is populated by `task build` (or the Docker build) from
// web/dist. In a fresh checkout it only contains .gitkeep; the server then
// answers "/" with a short notice instead of the app.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built frontend rooted at its top directory.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed pattern guarantees "dist" exists
	}
	return sub
}

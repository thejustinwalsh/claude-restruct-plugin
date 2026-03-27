package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// MountSPA mounts an embedded filesystem as a single-page app.
// Non-file requests (no extension) serve index.html for client-side routing.
func MountSPA(fsys fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(fsys))

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Try to open the file — if it exists, serve it
		if path != "" {
			if f, err := fsys.Open(path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Fallback: serve index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}

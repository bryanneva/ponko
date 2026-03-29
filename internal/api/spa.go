package api

import (
	"io/fs"
	"net/http"
	"strings"
)

func spaHandler(embeddedDist fs.FS) http.HandlerFunc {
	distFS, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		panic("failed to create sub filesystem: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(distFS))

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(distFS, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for non-file paths
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}

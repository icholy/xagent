package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed webui
var webuiFS embed.FS

// WebUI serves the React SPA from the embedded webui directory.
// It handles SPA routing by returning index.html for any path that doesn't
// exist as a file (client-side routing support).
func WebUI() http.Handler {
	webui, err := fs.Sub(webuiFS, "webui")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(webui))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Serve file if it exists
		if path != "" {
			if _, err := fs.Stat(webui, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback
		http.ServeFileFS(w, r, webui, "index.html")
	})
}

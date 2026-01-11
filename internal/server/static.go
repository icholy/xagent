package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed webui
var webuiFS embed.FS

// WebUI serves the React SPA from the embedded webui directory.
// It handles SPA routing by returning index.html for any path that doesn't
// exist as a file (client-side routing support).
func WebUI() http.Handler {
	// Get the webui subdirectory
	webui, err := fs.Sub(webuiFS, "webui")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(webui))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if the file exists
		if _, err := fs.Stat(webui, path[1:]); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// For SPA routing: if the path doesn't exist and doesn't look like
		// a static asset, serve index.html for client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

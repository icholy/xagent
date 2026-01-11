package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var distFS embed.FS

// staticHandler serves the React SPA from the embedded dist directory.
// It handles SPA routing by returning index.html for any path that doesn't
// exist as a file (client-side routing support).
func staticHandler() http.Handler {
	// Get the dist subdirectory
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if the file exists
		if _, err := fs.Stat(dist, path[1:]); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// For SPA routing: if the path doesn't exist and doesn't look like
		// a static asset, serve index.html for client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

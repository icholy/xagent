package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed webui/dist/*
var webUIFS embed.FS

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	// Get the subdirectory fs rooted at webui/dist
	distFS, err := fs.Sub(webUIFS, "webui/dist")
	if err != nil {
		http.Error(w, "Failed to load UI", http.StatusInternalServerError)
		return
	}

	// Strip /ui prefix from path
	path := strings.TrimPrefix(r.URL.Path, "/ui")
	if path == "" {
		path = "/"
	}

	// Try to serve the requested file
	// For SPA routing, if the file doesn't exist and it's not an asset, serve index.html
	fsHandler := http.FileServer(http.FS(distFS))

	// Check if the path looks like a static asset
	isAsset := strings.HasPrefix(path, "/assets/") ||
		strings.HasSuffix(path, ".js") ||
		strings.HasSuffix(path, ".css") ||
		strings.HasSuffix(path, ".svg") ||
		strings.HasSuffix(path, ".png") ||
		strings.HasSuffix(path, ".ico")

	if isAsset {
		// Serve static assets directly
		r.URL.Path = path
		fsHandler.ServeHTTP(w, r)
	} else {
		// For non-asset paths (routes), serve index.html for SPA routing
		r.URL.Path = "/"
		fsHandler.ServeHTTP(w, r)
	}
}

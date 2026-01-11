package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:webui
var webuiFS embed.FS

var webui fs.FS
var webuiBuilt bool

func init() {
	var err error
	webui, err = fs.Sub(webuiFS, "webui")
	if err != nil {
		panic(err)
	}
	_, err = fs.Stat(webui, "index.html")
	webuiBuilt = err == nil
}

// WebUI serves the React SPA from the embedded webui directory.
// It handles SPA routing by returning index.html for any path that doesn't
// exist as a file (client-side routing support).
func WebUI() http.Handler {
	if !webuiBuilt {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Frontend not built. Run 'mise run build' or 'cd webui && npm run build' to build the frontend.", http.StatusInternalServerError)
		})
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

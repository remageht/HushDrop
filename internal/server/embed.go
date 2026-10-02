package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed all:web
var embeddedFiles embed.FS

func getFileSystem() http.FileSystem {
	sub, err := fs.Sub(embeddedFiles, "web")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

// spaHandler serves static assets and falls back to index.html for SPA routes
type spaHandler struct {
	staticFS http.FileSystem
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Check if file exists in static FS
	f, err := h.staticFS.Open(strings.TrimPrefix(path, "/"))
	if err == nil {
		defer f.Close()
		stat, err := f.Stat()
		if err == nil && !stat.IsDir() {
			http.FileServer(h.staticFS).ServeHTTP(w, r)
			return
		}
	}

	// Fallback to index.html for SPA routing
	indexFile, err := h.staticFS.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer indexFile.Close()

	var modTime time.Time
	if stat, err := indexFile.Stat(); err == nil {
		modTime = stat.ModTime()
	} else {
		modTime = time.Now()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", modTime, indexFile)
}

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
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body style="font-family:sans-serif;padding:40px;background:#090d16;color:#f8fafc"><h2>HushDrop: Frontend Web Assets Not Built</h2><p>Please compile the React frontend using <code>scripts/build.ps1</code> or run <code>npm run build</code> in <code>frontend/</code> and sync to <code>internal/server/web/</code>.</p></body></html>`))
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

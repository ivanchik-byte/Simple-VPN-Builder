package web

import (
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// DistFS returns an fs.FS for the embedded React SPA dist directory.
func DistFS() fs.FS {
	sub, err := fs.Sub(EmbeddedFiles, "dist")
	if err != nil {
		panic("web: missing embedded dist directory: " + err.Error())
	}
	return sub
}

// DashboardV2 serves the React dashboard preview page.
func DashboardV2() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(DistFS(), "dashboard-v2.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

// DashboardAssets serves bundled JS/CSS for the dashboard preview.
func DashboardAssets() http.Handler {
	sub, err := fs.Sub(DistFS(), "assets")
	if err != nil {
		panic("web: missing embedded dist/assets directory: " + err.Error())
	}
	return http.StripPrefix("/ui/assets/", http.FileServer(http.FS(sub)))
}
func SPAHandler() http.Handler {
	distFs := DistFS()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(r.URL.Path, "/ui")
		cleanPath = strings.TrimPrefix(cleanPath, "/")

		if cleanPath == "" {
			cleanPath = "index.html"
		}

		// Try opening file
		data, err := fs.ReadFile(distFs, cleanPath)
		if err != nil {
			// Fallback to index.html
			cleanPath = "index.html"
			data, err = fs.ReadFile(distFs, cleanPath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
		}

		contentType := mime.TypeByExtension(filepath.Ext(cleanPath))
		if contentType == "" {
			contentType = "text/html; charset=utf-8"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}

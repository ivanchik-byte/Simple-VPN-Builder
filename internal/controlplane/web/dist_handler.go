package web

import (
	"io/fs"
	"net/http"
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

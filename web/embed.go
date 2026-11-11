package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const Base = "/app/"

//go:embed all:dist
var dist embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return NewHandler(sub)
}

func NewHandler(files fs.FS) http.Handler {
	static := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, Base)), "/")
		if name != "" && name != "index.html" && isFile(files, name) {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + name
			static.ServeHTTP(w, r2)
			return
		}
		if path.Ext(name) != "" && strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		index, err := fs.ReadFile(files, "index.html")
		if err != nil {
			http.Error(w, "SPA belum dibuild. Jalankan make web-build.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(index)
	})
}

func isFile(files fs.FS, name string) bool {
	info, err := fs.Stat(files, name)
	return err == nil && !info.IsDir()
}

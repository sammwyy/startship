package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:generate npm ci
//go:generate npm run build

//go:embed all:dist
var files embed.FS

func Handler() http.Handler {
	assets, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if len(r.URL.Path) > len("/assets/") && r.URL.Path[:len("/assets/")] == "/assets/" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServer(http.FS(assets)).ServeHTTP(w, r)
	})
}

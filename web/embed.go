package web

import (
	"embed"
	"net/http"
)

//go:embed *.html *.js *.css
var files embed.FS

func Handler() http.Handler { return http.FileServer(http.FS(files)) }

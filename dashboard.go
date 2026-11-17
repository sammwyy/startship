package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var dashboardFiles embed.FS

var dashboard = func() http.Handler {
	files, err := fs.Sub(dashboardFiles, "web")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(files))
}()

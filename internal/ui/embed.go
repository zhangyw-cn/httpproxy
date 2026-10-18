package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var dist embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/"), ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

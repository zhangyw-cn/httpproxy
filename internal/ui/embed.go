package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist/*
var dist embed.FS

func Handler(theme func() string) http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, "/")
		if r.URL.Path == "/" || !strings.Contains(trimmed, ".") {
			serveIndex(w, sub, themeAtRequest(theme))
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func themeAtRequest(theme func() string) string {
	if theme == nil {
		return "dark"
	}
	if theme() != "light" {
		return "dark"
	}
	return "light"
}

func serveIndex(w http.ResponseWriter, sub fs.FS, theme string) {
	raw, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "index.html", http.StatusInternalServerError)
		return
	}
	body := strings.Replace(string(raw), `<html lang="zh-CN">`, `<html lang="zh-CN" data-theme="`+theme+`">`, 1)
	body = stripThemeScript(body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func stripThemeScript(body string) string {
	const needle = "httpproxy.theme"
	at := strings.Index(body, needle)
	if at < 0 {
		return body
	}
	start := strings.LastIndex(body[:at], "<script")
	if start < 0 {
		return body
	}
	closeAt := strings.Index(body[at:], "</script>")
	if closeAt < 0 {
		return body
	}
	end := at + closeAt + len("</script>")
	return body[:start] + body[end:]
}

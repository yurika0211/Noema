// Package web 是灯織的最小 Web 样品。它由 noema -listen 提供，不另起服务。
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js trace.html trace.js
var files embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(files, ".")
	if err != nil {
		return http.NotFoundHandler()
	}
	filesHandler := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trace" {
			r.URL.Path = "/trace.html"
		}
		filesHandler.ServeHTTP(w, r)
	})
}

// Package web 是灯織的最小 Web 样品。它由 noema -listen 提供，不另起服务。
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js
var files embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(files, ".")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}

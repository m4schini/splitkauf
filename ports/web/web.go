// SPDX-License-Identifier: CC0-1.0

// Package web embeds and serves the built React frontend (see frontend/,
// output to ports/web/dist by `npm run build`). Handler serves static assets
// and falls back to index.html for client-side routes, so React Router
// (or similar) can own any path that isn't a real file.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded frontend build. Any request path that maps to
// a real file in dist is served as-is; any other path without a file
// extension falls back to index.html (SPA client-side routing). Paths with a
// file extension that don't exist in dist return 404, so missing assets
// don't silently serve the app shell.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("ports/web: dist not embedded: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic("ports/web: dist/index.html missing: " + err.Error())
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "." {
			name = "index.html"
		}

		if _, statErr := fs.Stat(sub, name); statErr != nil {
			if path.Ext(name) != "" {
				http.NotFound(w, r)

				return
			}
			// No file at this path and no extension: treat as a client-side
			// route and serve the app shell.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(index)

			return
		}

		fileServer.ServeHTTP(w, r)
	})
}

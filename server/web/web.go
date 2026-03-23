package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

// FrameTemplate returns the embedded frame-template.html content.
func FrameTemplate() string {
	data, _ := fs.ReadFile(staticFiles, "static/frame-template.html")
	return string(data)
}

// Handler returns an http.Handler that serves the embedded static files.
// All unmatched paths serve index.html (SPA fallback).
func Handler() http.Handler {
	sub, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		} else {
			path = path[1:] // strip leading /
		}

		// Check if file exists in embedded FS
		if f, err := sub.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for unmatched paths
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

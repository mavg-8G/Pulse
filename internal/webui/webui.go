// Package webui sirve el frontend compilado (Vite) embebido en el binario.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler sirve el frontend embebido.
func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	return HandlerFS(sub)
}

// HandlerFS sirve un frontend desde cualquier sistema de archivos (los tests usan uno en memoria).
func HandlerFS(sub fs.FS) http.Handler {
	files := http.FileServer(http.FS(sub))
	serveIndex := func(w http.ResponseWriter) {
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "Frontend no compilado. Ejecuta: make web", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		// index.html se sirve directo: http.FileServer redirige /index.html → / y el
		// service worker rechaza respuestas redirigidas al precachear.
		if p == "" || p == "index.html" {
			serveIndex(w)
			return
		}
		if st, err := fs.Stat(sub, p); err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // nombres con hash
			} else {
				w.Header().Set("Cache-Control", "no-cache") // sw.js, manifest, íconos: siempre revalidar
			}
			files.ServeHTTP(w, r)
			return
		}
		// Páginas estáticas sin JavaScript (/privacidad, /terminos): deben leerse sin sesión
		// y sin ejecutar la app, p. ej. por el revisor de Google.
		if path.Ext(p) == "" && !strings.Contains(p, "/") {
			if st, err := fs.Stat(sub, p+".html"); err == nil && !st.IsDir() {
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFileFS(w, r, sub, p+".html")
				return
			}
		}
		// Rutas del SPA (/movimientos, /plan…) → index.html. Archivos inexistentes con extensión → 404.
		if path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
		serveIndex(w)
	})
}

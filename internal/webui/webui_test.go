package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var fake = fstest.MapFS{
	"index.html":       {Data: []byte("<!doctype html>INDEX")},
	"sw.js":            {Data: []byte("self.x=1")},
	"assets/app-1a.js": {Data: []byte("console.log(1)")},
	"privacidad.html":  {Data: []byte("<!doctype html>PRIVACIDAD")},
}

func get(p string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	HandlerFS(fake).ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
	return rec
}

func TestRutas(t *testing.T) {
	cases := []struct {
		path, body, cache string
		code              int
	}{
		{"/", "INDEX", "no-cache", 200},
		{"/index.html", "INDEX", "no-cache", 200},
		{"/index.html?__WB_REVISION__=abc", "INDEX", "no-cache", 200}, // precache del service worker
		{"/movimientos", "INDEX", "no-cache", 200},
		{"/ajustes/cuentas", "INDEX", "no-cache", 200},
		{"/sw.js", "self.x=1", "no-cache", 200},
		{"/assets/app-1a.js", "console.log(1)", "immutable", 200},
		{"/assets/no-existe.js", "", "", 404},
		{"/privacidad", "PRIVACIDAD", "no-cache", 200}, // página legal estática, sin JS
		{"/terminos", "INDEX", "no-cache", 200},        // sin .html propio → SPA
		{"/ajustes/privacidad", "INDEX", "", 200},      // solo en la raíz
		{"/../../etc/passwd", "INDEX", "", 200},        // la ruta se limpia: nunca sale del FS embebido
	}
	for _, c := range cases {
		rec := get(c.path)
		if rec.Code != c.code {
			t.Errorf("%s: estado %d, esperaba %d", c.path, rec.Code, c.code)
			continue
		}
		if c.body != "" && !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: cuerpo %q", c.path, rec.Body.String())
		}
		if c.cache != "" && !strings.Contains(rec.Header().Get("Cache-Control"), c.cache) {
			t.Errorf("%s: Cache-Control %q, esperaba %q", c.path, rec.Header().Get("Cache-Control"), c.cache)
		}
	}
}

func TestSinBuildAvisa(t *testing.T) {
	rec := httptest.NewRecorder()
	HandlerFS(fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 503 {
		t.Fatalf("sin frontend compilado debe dar 503: %d", rec.Code)
	}
}

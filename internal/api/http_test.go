package api_test

import (
	"encoding/base32"
	"strings"
	"testing"

	"pulse/internal/db"
)

func mustB32(t *testing.T, s string) []byte {
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCSRF(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	body := map[string]any{"name": "Banco", "type": "bank"}
	cases := map[string][]reqOpt{
		"sin Origin ni Sec-Fetch-Site": {noOrigin},
		"Origin ajeno":                 {withOrigin("https://evil.example")},
		"Origin parecido":              {withOrigin("https://pulse.test.evil.example")},
		"Sec-Fetch-Site cross-site":    {noOrigin, withHeader("Sec-Fetch-Site", "cross-site")},
		"same-site no basta":           {withHeader("Sec-Fetch-Site", "same-site")},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if r := c.do("POST", "/api/accounts", body, opts...); r.Code != 403 || r.errCode() != "csrf" {
				t.Fatalf("esperaba 403 csrf, obtuve %d %s", r.Code, r.Raw)
			}
		})
	}
	c.must(201, "POST", "/api/accounts", body, noOrigin, withHeader("Sec-Fetch-Site", "same-origin"))
}

func TestCabecerasDeSeguridad(t *testing.T) {
	e := newEnv(t)
	r := e.client().do("GET", "/api/auth/state", nil)
	want := map[string]string{
		"Content-Security-Policy":    "default-src 'self'",
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "no-referrer",
		"Strict-Transport-Security":  "max-age=63072000",
		"Cache-Control":              "no-store",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for h, v := range want {
		if !strings.Contains(r.Headers.Get(h), v) {
			t.Errorf("%s = %q, esperaba contener %q", h, r.Headers.Get(h), v)
		}
	}
	if strings.Contains(r.Headers.Get("Content-Security-Policy"), "unsafe-inline") {
		t.Error("la CSP no debe permitir unsafe-inline")
	}
}

func TestCookieSegura(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	r := c.must(201, "POST", "/api/auth/register", map[string]any{"email": "a@x.com", "password": "una-clave-larga-y-segura", "invite_code": e.invite("")})
	sc := r.Headers.Get("Set-Cookie")
	for _, want := range []string{"__Host-pulse=", "Path=/", "HttpOnly", "Secure", "SameSite=Strict"} {
		if !strings.Contains(sc, want) {
			t.Errorf("la cookie no tiene %s: %s", want, sc)
		}
	}
	if strings.Contains(sc, "Domain") {
		t.Error("la cookie no debe tener Domain")
	}
}

func TestEntradaMalformada(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	if r := c.do("POST", "/api/accounts", map[string]any{"name": "x", "type": "bank", "admin": true}); r.Code != 400 {
		t.Fatalf("campos desconocidos deben rechazarse: %d", r.Code)
	}
	if r := c.do("POST", "/api/accounts", `{"name":"x","type":"bank"}`, withContentType("text/plain")); r.Code != 415 {
		t.Fatalf("Content-Type distinto de JSON debe dar 415: %d", r.Code)
	}
	if r := c.do("POST", "/api/accounts", `{"name":"`+strings.Repeat("a", 70000)+`"}`); r.Code != 413 {
		t.Fatalf("cuerpo enorme debe dar 413: %d", r.Code)
	}
	if r := c.do("POST", "/api/accounts", `{"name":"x","type":"bank"} {"x":1}`); r.Code != 400 {
		t.Fatalf("JSON con basura al final debe dar 400: %d", r.Code)
	}
	if r := c.do("GET", "/api/no-existe", nil); r.Code != 404 || r.errCode() != "not_found" {
		t.Fatalf("ruta API desconocida: %d %s", r.Code, r.Raw)
	}
}

func TestIPRealSoloDesdeProxyConfiable(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.email, c.pw = "a@x.com", "una-clave-larga-y-segura"
	// Desde una IP no confiable, X-Forwarded-For se ignora: no sirve para evadir el límite
	for i := 0; i < 10; i++ {
		c.do("POST", "/api/auth/login", map[string]any{"email": "z@x.com", "password": "x"}, withHeader("X-Forwarded-For", "1.1.1."+string(rune('0'+i))))
	}
	if r := c.do("POST", "/api/auth/login", map[string]any{"email": "z@x.com", "password": "x"}, withHeader("X-Forwarded-For", "9.9.9.9")); r.Code != 429 {
		t.Fatalf("X-Forwarded-For de un cliente no debe evadir el límite: %d", r.Code)
	}
	// Desde el proxy (10.0.0.0/8), se respeta la IP del cliente real
	p := e.client()
	p.ip = "10.0.0.2"
	if r := p.do("POST", "/api/auth/login", map[string]any{"email": "z@x.com", "password": "x"}, withHeader("X-Forwarded-For", "9.9.9.9")); r.Code != 401 {
		t.Fatalf("cliente distinto detrás del proxy no debe estar limitado: %d", r.Code)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	latest, _ := db.Latest()
	r := e.client().do("GET", "/healthz", nil)
	if r.Code != 200 || r.str("status") != "ok" || r.Body["schema"] != float64(latest) || r.Body["gmail"] != false || r.Body["push"] != false {
		t.Fatalf("healthz: %d %s", r.Code, r.Raw)
	}
	// Una base con el esquema atrasado (p. ej. un respaldo viejo restaurado sin migrar) no está sana.
	if _, err := e.st.DB.Exec(`DELETE FROM schema_migrations WHERE version = ?`, latest); err != nil {
		t.Fatal(err)
	}
	if r := e.client().do("GET", "/healthz", nil); r.Code != 503 || r.str("status") != "error" {
		t.Fatalf("healthz con esquema atrasado: %d %s", r.Code, r.Raw)
	}
}

func TestRutasDelFrontend(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	for _, p := range []string{"/", "/movimientos", "/plan"} {
		if r := c.do("GET", p, nil); r.Code != 200 || r.Raw != "SPA" {
			t.Errorf("%s: %d %q", p, r.Code, r.Raw)
		}
	}
	if r := c.do("POST", "/movimientos", map[string]any{}); r.Code != 405 {
		t.Errorf("POST a una ruta del frontend: %d", r.Code)
	}
	if r := c.do("GET", "/api/nada", nil); r.Code != 404 || r.Raw == "SPA" {
		t.Error("las rutas /api desconocidas no deben caer en el frontend")
	}
	// El frontend también lleva CSP
	if r := c.do("GET", "/", nil); !strings.Contains(r.Headers.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("falta CSP en el frontend")
	}
}

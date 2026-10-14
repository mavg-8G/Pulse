package api_test

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pulse/internal/admin"
	"pulse/internal/api"
	"pulse/internal/auth"
	"pulse/internal/clock"
	"pulse/internal/config"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/store"
)

const origin = "https://pulse.test"

type env struct {
	t     *testing.T
	h     http.Handler
	st    *store.Store
	keys  *crypto.Keyring
	clock *clock.Fake
}

// 14 oct 2026 10:00 en Guayaquil
var t0 = time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC)

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "pulse.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(func(k string) string {
		return map[string]string{"PULSE_BASE_URL": origin, "PULSE_TRUSTED_PROXY": "10.0.0.0/8"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := crypto.NewKeyring(bytes.Repeat([]byte{42}, 32))
	c := clock.NewFake(t0)
	st := &store.Store{DB: d, Clock: c}
	srv := api.New(api.Deps{Config: cfg, Store: st, Keys: keys, Hasher: &auth.Hasher{Keys: keys, Iterations: 1000}, Clock: c,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Static: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("SPA")) })})
	return &env{t: t, h: srv.Handler(), st: st, keys: keys, clock: c}
}

func (e *env) invite(email string) string {
	code, _, err := admin.CreateInvite(context.Background(), e.st, e.keys, email, "user", 72*time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return code
}

type client struct {
	e      *env
	cookie string
	ip     string
	totp   []byte
	email  string
	pw     string
}

func (e *env) client() *client { return &client{e: e, ip: "203.0.113.7"} }

type resp struct {
	Code    int
	Body    map[string]any
	List    []any
	Raw     string
	Headers http.Header
}

func (r *resp) errCode() string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (r *resp) str(k string) string { s, _ := r.Body[k].(string); return s }

type reqOpt func(*http.Request)

func noOrigin(r *http.Request)      { r.Header.Del("Origin") }
func withOrigin(o string) reqOpt    { return func(r *http.Request) { r.Header.Set("Origin", o) } }
func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withContentType(ct string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Content-Type", ct) }
}

func (c *client) do(method, path string, body any, opts ...reqOpt) *resp {
	c.e.t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = c.ip + ":5555"
	req.Header.Set("Origin", origin)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-pulse", Value: c.cookie})
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	c.e.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "__Host-pulse" {
			if ck.MaxAge < 0 || ck.Value == "" {
				c.cookie = ""
			} else {
				c.cookie = ck.Value
			}
		}
	}
	r := &resp{Code: rec.Code, Raw: rec.Body.String(), Headers: rec.Header()}
	if strings.HasPrefix(strings.TrimSpace(r.Raw), "[") {
		json.Unmarshal(rec.Body.Bytes(), &r.List)
	} else {
		json.Unmarshal(rec.Body.Bytes(), &r.Body)
	}
	return r
}

func (c *client) must(want int, method, path string, body any, opts ...reqOpt) *resp {
	c.e.t.Helper()
	r := c.do(method, path, body, opts...)
	if r.Code != want {
		c.e.t.Fatalf("%s %s: estado %d, esperaba %d: %s", method, path, r.Code, want, r.Raw)
	}
	return r
}

func (c *client) totpCode() string { return auth.TOTPCode(c.totp, c.e.clock.Now()) }

// nextStep avanza el reloj 30 s para obtener un código TOTP nuevo (el anterior no se puede reutilizar).
func (c *client) nextStep() { c.e.clock.Advance(30 * time.Second) }

// signup registra un usuario completo (contraseña + TOTP) y devuelve el cliente con sesión completa.
func (e *env) signup(email string) *client {
	e.t.Helper()
	c := e.client()
	c.email, c.pw = email, "una-clave-larga-y-segura"
	c.must(201, "POST", "/api/auth/register", map[string]any{"email": email, "password": c.pw, "invite_code": e.invite(""), "timezone": "America/Guayaquil"})
	r := c.must(200, "POST", "/api/auth/enroll/totp/begin", nil)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(r.str("secret"))
	if err != nil {
		e.t.Fatal(err)
	}
	c.totp = secret
	r = c.must(200, "POST", "/api/auth/enroll/totp/finish", map[string]any{"code": c.totpCode()})
	if codes, _ := r.Body["recovery_codes"].([]any); len(codes) != 10 {
		e.t.Fatalf("esperaba 10 códigos de recuperación: %s", r.Raw)
	}
	return c
}

func (c *client) stepUp() {
	c.e.t.Helper()
	c.nextStep()
	c.must(200, "POST", "/api/auth/step-up/totp", map[string]any{"code": c.totpCode()})
}

func (c *client) login() *resp {
	c.e.t.Helper()
	c.cookie = ""
	r := c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	c.nextStep()
	c.must(200, "POST", "/api/auth/mfa/totp", map[string]any{"code": c.totpCode()})
	return r
}

func (c *client) firstID(path string) string {
	c.e.t.Helper()
	r := c.must(200, "GET", path, nil)
	if len(r.List) == 0 {
		c.e.t.Fatalf("lista vacía en %s", path)
	}
	return r.List[0].(map[string]any)["id"].(string)
}

func (c *client) categoryID(name string) string {
	c.e.t.Helper()
	for _, x := range c.must(200, "GET", "/api/categories", nil).List {
		m := x.(map[string]any)
		if m["name"] == name {
			return m["id"].(string)
		}
	}
	c.e.t.Fatalf("no existe la categoría %s", name)
	return ""
}

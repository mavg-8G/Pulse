package api_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"pulse/internal/api"
	"pulse/internal/auth"
	"pulse/internal/config"
	"pulse/internal/gmail/gmailtest"
)

// withFakeGoogle monta el servidor sin credenciales de Google, pero apuntando al Google falso:
// así se prueba la configuración desde Ajustes de principio a fin.
func withFakeGoogle(t *testing.T, e *env) (*gmailtest.Server, func() *api.Server) {
	t.Helper()
	g := gmailtest.New()
	t.Cleanup(g.Close)
	cfg, err := config.Load(func(k string) string { return map[string]string{"PULSE_BASE_URL": origin}[k] })
	if err != nil {
		t.Fatal(err)
	}
	ep := g.Endpoints()
	build := func() *api.Server {
		return api.New(api.Deps{Config: cfg, Store: e.st, Keys: e.keys, Hasher: &auth.Hasher{Keys: e.keys, Iterations: 1000}, Clock: e.clock, GoogleEP: &ep,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	}
	e.h = build().Handler()
	return g, build
}

func (c *client) makeAdmin() {
	c.e.t.Helper()
	id := c.must(200, "GET", "/api/me", nil).str("id")
	if _, err := c.e.st.DB.Exec(`UPDATE users SET role = 'admin' WHERE id = ?`, id); err != nil {
		c.e.t.Fatal(err)
	}
}

func TestGmailAdminConfiguraDesdeAjustes(t *testing.T) {
	e := newEnv(t)
	_, build := withFakeGoogle(t, e)
	adm := e.signup("admin@example.com")
	adm.makeAdmin()
	user := e.signup("user@example.com")

	// Un usuario normal no ve ni cambia la configuración, ni siquiera con step-up.
	user.must(403, "GET", "/api/admin/gmail", nil)
	user.stepUp()
	user.must(403, "PUT", "/api/admin/gmail", map[string]any{"client_id": gmailtest.ClientID, "client_secret": "x"})

	v := adm.must(200, "GET", "/api/admin/gmail", nil)
	if v.str("source") != "" || v.str("redirect_uri") != origin+"/ajustes/gmail" || v.str("webhook_url") != origin+"/api/gmail/webhook" {
		t.Fatalf("vista inicial: %s", v.Raw)
	}
	if user.must(200, "GET", "/api/gmail", nil).Body["available"] != false {
		t.Fatal("sin credenciales Gmail no debe estar disponible")
	}

	body := map[string]any{"client_id": gmailtest.ClientID, "client_secret": gmailtest.ClientSecret}
	if r := adm.do("PUT", "/api/admin/gmail", body); r.errCode() != "step_up_required" {
		t.Fatalf("guardar sin step-up: %s", r.Raw)
	}
	adm.stepUp()
	for _, bad := range []map[string]any{
		{"client_id": "cualquier-cosa", "client_secret": "s"},
		{"client_id": gmailtest.ClientID},
		{"client_id": gmailtest.ClientID, "client_secret": "s", "pubsub_topic": "gmail"},
		{"client_id": gmailtest.ClientID, "client_secret": "s", "pubsub_topic": "projects/pulse-123456/topics/gmail", "pubsub_sa": "yo@gmail.com"},
	} {
		if r := adm.do("PUT", "/api/admin/gmail", bad); r.Code != 400 {
			t.Fatalf("%v: esperaba 400, obtuve %d %s", bad, r.Code, r.Raw)
		}
	}
	v = adm.must(200, "PUT", "/api/admin/gmail", body)
	if v.str("source") != "ajustes" || v.Body["client_secret_set"] != true || bytes.Contains([]byte(v.Raw), []byte(gmailtest.ClientSecret)) {
		t.Fatalf("respuesta tras guardar: %s", v.Raw)
	}
	var raw []byte
	if err := e.st.DB.QueryRow(`SELECT value FROM server_settings WHERE key = 'google'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(gmailtest.ClientSecret)) || bytes.Contains(raw, []byte(gmailtest.ClientID)) {
		t.Fatal("las credenciales deben guardarse cifradas")
	}

	// Aplicado en caliente: cualquier usuario ya puede conectar su Gmail.
	st := user.must(200, "GET", "/api/gmail", nil)
	if st.Body["available"] != true || st.Body["realtime"] != false || st.Body["gemini_available"] != false {
		t.Fatalf("estado tras configurar: %s", st.Raw)
	}
	(&gmailEnv{}).connectGmail(t, user, "user@gmail.com")

	// Mismo cliente sin secreto: se conserva el anterior. Gemini y Pub/Sub se añaden.
	adm.must(200, "PUT", "/api/admin/gmail", map[string]any{"client_id": gmailtest.ClientID, "gemini_key": "k",
		"pubsub_topic": "projects/pulse-123456/topics/gmail", "pubsub_sa": pubsubSA})
	st = user.must(200, "GET", "/api/gmail", nil)
	if st.Body["connected"] != true || st.Body["realtime"] != true || st.Body["gemini_available"] != true {
		t.Fatalf("estado tras ampliar: %s", st.Raw)
	}
	user.must(200, "POST", "/api/gmail/sync", nil) // el secreto conservado sigue funcionando
	// Un cliente distinto sí exige su secreto.
	adm.must(400, "PUT", "/api/admin/gmail", map[string]any{"client_id": "otro.apps.googleusercontent.com"})

	// Tras reiniciar, la configuración guardada sigue activa.
	e.h = build().Handler()
	if v := adm.must(200, "GET", "/api/admin/gmail", nil); v.str("source") != "ajustes" || v.Body["gemini_key_set"] != true {
		t.Fatalf("tras reiniciar: %s", v.Raw)
	}

	adm.must(200, "PUT", "/api/admin/gmail", map[string]any{"client_id": gmailtest.ClientID, "gemini_key": "-"})
	if user.must(200, "GET", "/api/gmail", nil).Body["gemini_available"] != false {
		t.Fatal("\"-\" debe quitar la clave de Gemini")
	}

	adm.must(200, "DELETE", "/api/admin/gmail", nil)
	if user.must(200, "GET", "/api/gmail", nil).Body["available"] != false {
		t.Fatal("al borrar la configuración (sin variables de entorno) Gmail queda desactivado")
	}

	var events int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE event IN ('gmail_server_configured', 'gmail_server_cleared')`).Scan(&events)
	if events != 4 {
		t.Fatalf("eventos de auditoría: %d", events)
	}
}

// RunGmail arranca y detiene la sincronización en segundo plano al cambiar las credenciales.
func TestGmailRunAplicaCambiosEnCaliente(t *testing.T) {
	e := newEnv(t)
	_, build := withFakeGoogle(t, e)
	srv := build()
	e.h = srv.Handler()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); srv.RunGmail(ctx) }()

	adm := e.signup("admin2@example.com")
	adm.makeAdmin()
	adm.stepUp()
	for i := 0; i < 3; i++ { // reemplazar la integración varias veces no deja goroutines colgadas
		adm.must(200, "PUT", "/api/admin/gmail", map[string]any{"client_id": gmailtest.ClientID, "client_secret": gmailtest.ClientSecret})
	}
	adm.must(200, "DELETE", "/api/admin/gmail", nil)
	cancel()
	<-done
}

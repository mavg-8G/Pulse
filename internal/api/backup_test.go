package api_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"

	"pulse/internal/api"
	"pulse/internal/auth"
	"pulse/internal/backup"
	"pulse/internal/config"
)

// fakeBucket acepta PUT firmados y puede fallar a pedido.
type fakeBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	fail    int // respuestas 503 pendientes
}

func (b *fakeBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.Method != http.MethodPut || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		w.WriteHeader(403)
		return
	}
	if b.fail > 0 {
		b.fail--
		w.WriteHeader(503)
		return
	}
	data, _ := io.ReadAll(r.Body)
	b.objects[r.URL.Path] = data
}

func withBackup(t *testing.T, e *env) (*fakeBucket, *backup.Service, *age.X25519Identity) {
	t.Helper()
	bucket := &fakeBucket{objects: map[string][]byte{}}
	hs := httptest.NewServer(bucket)
	t.Cleanup(hs.Close)
	ep, _ := url.Parse(hs.URL)
	id, _ := age.GenerateX25519Identity()
	svc := &backup.Service{DB: e.st.DB, Store: e.st, Recipients: []age.Recipient{id.Recipient()}, Dir: filepath.Join(t.TempDir(), "backups"),
		Dest:  &backup.S3{Endpoint: ep, Region: "us-east-1", Bucket: "b", Prefix: "pulse/", AccessKey: "AK", SecretKey: "SK", PathStyle: true, Client: hs.Client()},
		Clock: e.clock, Interval: 24 * time.Hour, Attempts: 2,
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	cfg, err := config.Load(func(k string) string {
		return map[string]string{"PULSE_BASE_URL": origin, "PULSE_TRUSTED_PROXY": "10.0.0.0/8"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(api.Deps{Config: cfg, Store: e.st, Keys: e.keys, Hasher: &auth.Hasher{Keys: e.keys, Iterations: 1000}, Clock: e.clock,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Backup: svc})
	svc.Alert = srv.BackupAlert
	e.h = srv.Handler()
	return bucket, svc, id
}

func health(c *client) string {
	c.e.t.Helper()
	r := c.e.client().do("GET", "/healthz", nil, noOrigin)
	s, _ := r.Body["backup"].(string)
	return s
}

func TestRespaldoSoloAdminConStepUp(t *testing.T) {
	e := newEnv(t)
	adm := e.signup("admin@example.com")
	user := e.signup("user@example.com")
	if health(adm) != "off" {
		t.Fatal("sin configurar, /healthz debe decir off")
	}
	adm.makeAdmin()
	adm.stepUp()
	if r := adm.do("POST", "/api/admin/backups", nil); r.errCode() != "backup_off" {
		t.Fatalf("sin destino: %s", r.Raw)
	}

	bucket, _, id := withBackup(t, e)
	adm.login()
	user.login()
	if health(adm) != "pending" {
		t.Fatal("configurado y sin respaldos: pending")
	}

	// Un usuario normal no ve el estado, ni respalda ni descarga, ni siquiera con step-up.
	user.must(403, "GET", "/api/admin/backups", nil)
	user.stepUp()
	user.must(403, "POST", "/api/admin/backups", nil)
	user.must(403, "GET", "/api/admin/backups/latest", nil)

	st := adm.must(200, "GET", "/api/admin/backups", nil)
	if st.Body["configured"] != true || strings.Contains(st.Raw, "SK") || strings.Contains(st.Raw, "AK") {
		t.Fatalf("estado sin credenciales: %s", st.Raw)
	}
	// Sin step-up reciente no se respalda ni se descarga.
	for _, p := range []struct{ m, path string }{{"POST", "/api/admin/backups"}, {"GET", "/api/admin/backups/latest"}} {
		if r := adm.do(p.m, p.path, nil); r.errCode() != "step_up_required" {
			t.Fatalf("%s %s sin step-up: %d %s", p.m, p.path, r.Code, r.Raw)
		}
	}
	if len(bucket.objects) != 0 {
		t.Fatal("no debió subir nada")
	}
	adm.stepUp()
	adm.must(404, "GET", "/api/admin/backups/latest", nil) // todavía no hay copia

	run := adm.must(200, "POST", "/api/admin/backups", nil)
	if run.str("status") != "ok" || run.Body["size_bytes"].(float64) <= 0 || len(run.str("sha256")) != 64 {
		t.Fatalf("respaldo: %s", run.Raw)
	}
	if len(bucket.objects) != 1 || health(adm) != "ok" {
		t.Fatalf("objetos %d, salud %s", len(bucket.objects), health(adm))
	}
	for _, obj := range bucket.objects {
		if bytes.Contains(obj, []byte("SQLite format 3")) || bytes.Contains(obj, []byte("admin@example.com")) {
			t.Fatal("el objeto subido no está cifrado")
		}
	}

	// La descarga es la copia cifrada: igual a la subida y legible solo con la identidad age.
	dl := adm.must(200, "GET", "/api/admin/backups/latest", nil)
	if dl.Headers.Get("Content-Type") != "application/octet-stream" || !strings.Contains(dl.Headers.Get("Content-Disposition"), ".db.age") {
		t.Fatalf("cabeceras: %v", dl.Headers)
	}
	for _, obj := range bucket.objects {
		if dl.Raw != string(obj) {
			t.Fatal("la descarga no es la copia subida")
		}
	}
	if _, err := age.Decrypt(strings.NewReader(dl.Raw), id); err != nil {
		t.Fatalf("no descifra con la identidad: %v", err)
	}
	other, _ := age.GenerateX25519Identity()
	if _, err := age.Decrypt(strings.NewReader(dl.Raw), other); err == nil {
		t.Fatal("otra identidad no debe poder descifrarla")
	}

	// Queda en la actividad de seguridad del admin.
	act := adm.must(200, "GET", "/api/security/activity", nil).Raw
	for _, ev := range []string{"backup_manual", "backup_downloaded"} {
		if !strings.Contains(act, `"`+ev+`"`) {
			t.Errorf("falta %s en la actividad", ev)
		}
	}

	// Destino caído: el resultado dice por qué y /healthz pasa a failing.
	bucket.fail = 5
	run = adm.must(200, "POST", "/api/admin/backups", nil)
	if run.str("status") != "failed" || run.Body["attempts"].(float64) != 2 || !strings.Contains(run.str("error"), "503") {
		t.Fatalf("fallo: %s", run.Raw)
	}
	if health(adm) != "failing" {
		t.Fatal("tras un fallo, /healthz debe decir failing")
	}
	// El historial muestra fecha, tamaño y resultado de cada intento.
	st = adm.must(200, "GET", "/api/admin/backups", nil)
	if runs, _ := st.Body["runs"].([]any); len(runs) != 2 || st.Body["last_ok"] == nil || st.Body["download"] != true {
		t.Fatalf("historial: %s", st.Raw)
	}
}

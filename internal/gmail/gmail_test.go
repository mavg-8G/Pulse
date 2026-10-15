package gmail_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pulse/internal/auth"
	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/gmail"
	"pulse/internal/gmail/gmailtest"
	"pulse/internal/importer"
	"pulse/internal/mailparse"
	"pulse/internal/store"
)

func parse(t *testing.T, raw string) *mailparse.Email {
	t.Helper()
	e, err := mailparse.ParseEML(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

const redirect = "https://pulse.test/ajustes/gmail"

type env struct {
	t        *testing.T
	st       *store.Store
	keys     *crypto.Keyring
	c        *clock.Fake
	g        *gmailtest.Server
	s        *gmail.Syncer
	dbPath   string
	mu       sync.Mutex
	imported []*importer.Outcome
	broken   []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pulse.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC))
	keys, _ := crypto.NewKeyring(bytes.Repeat([]byte{7}, 32))
	st := &store.Store{DB: d, Clock: c}
	g := gmailtest.New()
	t.Cleanup(g.Close)
	e := &env{t: t, st: st, keys: keys, c: c, g: g, dbPath: path}
	e.s = &gmail.Syncer{Store: st, Keys: keys, Clock: c, Interval: 15 * time.Minute,
		Client:   &gmail.Client{ClientID: gmailtest.ClientID, ClientSecret: gmailtest.ClientSecret, RedirectURL: redirect, EP: g.Endpoints(), HTTP: gmail.NewHTTPClient()},
		Importer: &importer.Importer{Store: st, Keys: keys, Clock: c, DefaultTZ: "America/Guayaquil"},
		OnImported: func(_ context.Context, _ *store.User, out *importer.Outcome) {
			e.mu.Lock()
			e.imported = append(e.imported, out)
			e.mu.Unlock()
		},
		OnBroken: func(_ context.Context, uid, code string) {
			e.mu.Lock()
			e.broken = append(e.broken, code)
			e.mu.Unlock()
		},
	}
	return e
}

func (e *env) user() *store.User {
	e.t.Helper()
	uid := auth.NewID()
	now := store.TS(e.c.Now())
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, role, dek_enc, timezone, created_at) VALUES (?, ?, 'x', 'user', ?, 'America/Guayaquil', ?)`,
	} {
		if _, err := e.st.DB.Exec(q, uid, uid+"@example.com", e.keys.NewDEK(uid), now); err != nil {
			e.t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name string
		sys  int
	}{{"Comida", 0}, {"Otros", 1}} {
		if _, err := e.st.DB.Exec(`INSERT INTO categories (id, user_id, name, is_system, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			auth.NewID(), uid, c.name, c.sys, now, now); err != nil {
			e.t.Fatal(err)
		}
	}
	u, err := e.st.User(context.Background(), uid)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *env) sealer(u *store.User) *crypto.Sealer {
	sl, err := e.keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		e.t.Fatal(err)
	}
	return sl
}

// connect hace el flujo OAuth completo como lo haría el navegador.
func (e *env) connect(u *store.User, email string) {
	e.t.Helper()
	state, challenge := gmail.NewState(e.keys.Lookup, u.ID, "sesion-1", e.c.Now())
	verifier, err := gmail.CheckState(e.keys.Lookup, state, u.ID, "sesion-1", e.c.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	code := e.g.Authorize(email, challenge, redirect)
	got, err := e.s.Connect(context.Background(), u, e.sealer(u), code, verifier)
	if err != nil || got != email {
		e.t.Fatalf("conectando: %q %v", got, err)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../mailparse/testdata/" + name + ".eml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *env) txCount(uid string) (total, pending int) {
	e.t.Helper()
	e.st.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'pending_review'), 0) FROM transactions WHERE user_id = ? AND source = 'gmail'`, uid).Scan(&total, &pending)
	return
}

func TestStateYPKCE(t *testing.T) {
	keys, _ := crypto.NewKeyring(bytes.Repeat([]byte{1}, 32))
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	state, challenge := gmail.NewState(keys.Lookup, "u1", "s1", now)
	v, err := gmail.CheckState(keys.Lookup, state, "u1", "s1", now.Add(9*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(v))
	if len(v) < 43 || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		t.Fatal("el desafío no corresponde al verificador S256")
	}
	if strings.Contains(state, v) {
		t.Fatal("el verificador PKCE no debe viajar en el state")
	}
	other, _ := crypto.NewKeyring(bytes.Repeat([]byte{2}, 32))
	parts := strings.SplitN(state, ".", 2)
	cases := map[string]func() error{
		"otro usuario": func() error { _, err := gmail.CheckState(keys.Lookup, state, "u2", "s1", now); return err },
		"otra sesión":  func() error { _, err := gmail.CheckState(keys.Lookup, state, "u1", "s2", now); return err },
		"caducado": func() error {
			_, err := gmail.CheckState(keys.Lookup, state, "u1", "s1", now.Add(11*time.Minute))
			return err
		},
		"otra clave":     func() error { _, err := gmail.CheckState(other.Lookup, state, "u1", "s1", now); return err },
		"firma cambiada": func() error { _, err := gmail.CheckState(keys.Lookup, parts[0]+".AAAA", "u1", "s1", now); return err },
		"sin firma":      func() error { _, err := gmail.CheckState(keys.Lookup, parts[0], "u1", "s1", now); return err },
		"basura":         func() error { _, err := gmail.CheckState(keys.Lookup, "x.y", "u1", "s1", now); return err },
	}
	for name, f := range cases {
		if !errors.Is(f(), gmail.ErrState) {
			t.Errorf("%s: debía rechazarse", name)
		}
	}
	s2, _ := gmail.NewState(keys.Lookup, "u1", "s1", now)
	if s2 == state {
		t.Fatal("cada state debe ser único")
	}
}

func TestConsultaSoloRemitentesBancarios(t *testing.T) {
	q := gmail.Query(2)
	for _, want := range []string{"from:(servicios@tarjetasbancopichincha.com OR bancaenlinea@produbanco.com OR servicios@titanium.com.ec)", "newer_than:2d"} {
		if !strings.Contains(q, want) {
			t.Fatalf("consulta %q sin %q", q, want)
		}
	}
}

func TestConectarYSincronizar(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "matias@gmail.com")
	ctx := context.Background()

	e.g.Deliver("matias@gmail.com", fixture(t, "produbanco-consumo"))
	e.g.Deliver("matias@gmail.com", fixture(t, "diners-compra-columnas"))
	e.g.Deliver("matias@gmail.com", fixture(t, "pichincha-pago"))
	personal := e.g.Deliver("matias@gmail.com", []byte("From: mama@example.com\r\nSubject: Almuerzo el domingo\r\nMessage-ID: <x@y>\r\n\r\nTe espero a las 13:00\r\n"))
	e.g.Deliver("matias@gmail.com", fixture(t, "pichincha-consumo"))

	r, err := e.s.SyncUser(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Found != 4 || r.Created != 3 || r.Ignored != 1 || r.Duplicate != 0 {
		t.Fatalf("resultado inesperado: %+v", r)
	}
	for _, id := range e.g.RawFetched {
		if id == personal {
			t.Fatal("Pulse descargó un correo que no es del banco")
		}
	}
	if len(e.g.Queries) < 2 || !strings.Contains(e.g.Queries[0], "newer_than:90d") {
		t.Fatalf("la primera sincronización debe mirar 90 días y paginar: %q", e.g.Queries)
	}
	if total, pending := e.txCount(u.ID); total != 3 || pending != 3 {
		t.Fatalf("gastos: %d (pendientes %d); modo sombra: todo por revisar", total, pending)
	}
	if len(e.imported) != 3 {
		t.Fatalf("OnImported llamado %d veces", len(e.imported))
	}
	// Ni el correo, ni el email de la cuenta, ni los tokens quedan en claro en la BD.
	e.st.DB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	raw, _ := os.ReadFile(e.dbPath)
	for _, secret := range []string{"matias@gmail.com", "PEDIDOSYA", "Pedidosya", "Almuerzo", "rt-", "at-", "Uber Eats", "Has realizado"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("la BD contiene %q en claro", secret)
		}
	}
	c, _ := e.st.Gmail(ctx, u.ID)
	if c.LastOKAt == nil || c.Failures != 0 || c.Imported != 3 || !c.NextSyncAt.Equal(e.c.Now().Add(15*time.Minute)) {
		t.Fatalf("estado tras sincronizar: %+v", c)
	}

	// Segunda vuelta: nada nuevo, nada se vuelve a descargar.
	before := len(e.g.RawFetched)
	r, err = e.s.SyncUser(ctx, u.ID)
	if err != nil || r.Created != 0 || r.Duplicate != 4 || len(e.g.RawFetched) != before {
		t.Fatalf("la segunda sincronización repitió trabajo: %+v %v", r, err)
	}
	if !strings.Contains(e.g.Queries[len(e.g.Queries)-1], "newer_than:2d") {
		t.Fatalf("después de una sincronización correcta basta con 2 días: %q", e.g.Queries[len(e.g.Queries)-1])
	}
}

// El mismo correo puede llegar por Pub/Sub y por polling a la vez: se registra una sola vez.
func TestConcurrenciaUnSoloGasto(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	e.g.Deliver("a@gmail.com", fixture(t, "produbanco-consumo"))
	e.g.Deliver("a@gmail.com", fixture(t, "diners-compra-filas"))
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.s.SyncUser(context.Background(), u.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if total, _ := e.txCount(u.ID); total != 2 {
		t.Fatalf("se registraron %d gastos, esperaba 2", total)
	}
	if len(e.g.RawFetched) != 2 {
		t.Fatalf("cada correo debe descargarse una vez; descargas: %d", len(e.g.RawFetched))
	}
}

// Aunque se pierda la tabla de vistos (o se reconecte), el hash del mensaje evita duplicados.
func TestReconectarNoDuplica(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	e.g.Deliver("a@gmail.com", fixture(t, "produbanco-consumo"))
	if _, err := e.s.SyncUser(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Disconnect(context.Background(), u, e.sealer(u)); err != nil {
		t.Fatal(err)
	}
	e.connect(u, "a@gmail.com")
	r, err := e.s.SyncUser(context.Background(), u.ID)
	if err != nil || r.Duplicate != 1 || r.Created != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if total, _ := e.txCount(u.ID); total != 1 {
		t.Fatal("se duplicó el gasto al reconectar")
	}
}

func TestAccesoRevocado(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	e.g.RevokeAll()
	_, err := e.s.SyncUser(context.Background(), u.ID)
	if !errors.Is(err, gmail.ErrBroken) {
		t.Fatalf("esperaba conexión rota, fue %v", err)
	}
	c, _ := e.st.Gmail(context.Background(), u.ID)
	if c.Status != "broken" || c.LastError != "sin_autorizacion" && c.LastError != "revocado" || len(e.broken) != 1 {
		t.Fatalf("estado: %+v avisos: %v", c, e.broken)
	}
	due, _ := e.st.DueGmail(context.Background())
	if len(due) != 0 {
		t.Fatal("una conexión rota no debe sincronizarse sola")
	}
	tokens := e.g.Count("token")
	if _, err := e.s.SyncUser(context.Background(), u.ID); !errors.Is(err, gmail.ErrBroken) || e.g.Count("token") != tokens {
		t.Fatal("con la conexión rota no se debe insistir con Google")
	}
	// Reconectar la repara.
	e.connect(u, "a@gmail.com")
	if c, _ := e.st.Gmail(context.Background(), u.ID); c.Status != "active" || c.LastError != "" {
		t.Fatalf("reconectar debe reactivar: %+v", c)
	}
}

func TestRefreshInvalidGrant(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	e.c.Advance(2 * time.Hour) // el access token en caché caducó
	e.g.Fail["token"] = []int{400}
	if _, err := e.s.SyncUser(context.Background(), u.ID); !errors.Is(err, gmail.ErrBroken) {
		t.Fatalf("invalid_grant debe romper la conexión: %v", err)
	}
	if c, _ := e.st.Gmail(context.Background(), u.ID); c.LastError != "revocado" {
		t.Fatalf("código: %q", c.LastError)
	}
}

// Un 401 de Gmail con token aún "vigente" (Google lo invalidó antes): se pide otro y se reintenta una vez.
func TestAccessTokenInvalidadoSeRenueva(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	e.g.Deliver("a@gmail.com", fixture(t, "produbanco-consumo"))
	e.g.ExpireAccess()
	r, err := e.s.SyncUser(context.Background(), u.ID)
	if err != nil || r.Created != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestEsperaExponencial(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	ctx := context.Background()
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute}
	e.g.Fail["list"] = []int{503, 503, 503}
	for i, d := range want {
		if _, err := e.s.SyncUser(ctx, u.ID); err == nil || !gmail.Temporary(err) {
			t.Fatalf("intento %d: esperaba error temporal, fue %v", i, err)
		}
		c, _ := e.st.Gmail(ctx, u.ID)
		if c.Status != "active" || c.Failures != i+1 || !c.NextSyncAt.Equal(e.c.Now().Add(d)) || c.LastError != "google_no_disponible" {
			t.Fatalf("intento %d: %+v (esperaba reintento en %s)", i, c, d)
		}
	}
	// 429 con Retry-After más largo que la espera calculada: manda Google.
	e.g.Fail["list"] = []int{429}
	e.g.RetryAfter = "900"
	e.s.SyncUser(ctx, u.ID)
	c, _ := e.st.Gmail(ctx, u.ID)
	if !c.NextSyncAt.Equal(e.c.Now().Add(15*time.Minute)) || c.LastError != "limite_de_google" {
		t.Fatalf("Retry-After ignorado: %+v", c)
	}
	// Al recuperarse, el contador vuelve a cero.
	e.g.RetryAfter = ""
	if _, err := e.s.SyncUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := e.st.Gmail(ctx, u.ID); c.Failures != 0 || c.LastError != "" {
		t.Fatalf("%+v", c)
	}
}

// Un fallo al descargar un mensaje no lo marca como visto: se reintenta en la siguiente vuelta.
func TestFalloAlDescargarSeReintenta(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	e.g.Deliver("a@gmail.com", fixture(t, "produbanco-consumo"))
	e.g.Fail["raw"] = []int{500}
	if _, err := e.s.SyncUser(context.Background(), u.ID); err == nil {
		t.Fatal("esperaba error")
	}
	r, err := e.s.SyncUser(context.Background(), u.ID)
	if err != nil || r.Created != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDosUsuariosAislados(t *testing.T) {
	e := newEnv(t)
	a, b := e.user(), e.user()
	e.connect(a, "a@gmail.com")
	e.connect(b, "b@gmail.com")
	e.g.Deliver("a@gmail.com", fixture(t, "produbanco-consumo"))
	for _, u := range []*store.User{a, b} {
		if _, err := e.s.SyncUser(context.Background(), u.ID); err != nil {
			t.Fatal(err)
		}
	}
	if ta, _ := e.txCount(a.ID); ta != 1 {
		t.Fatalf("A tiene %d gastos", ta)
	}
	if tb, _ := e.txCount(b.ID); tb != 0 {
		t.Fatal("un correo de A creó un gasto en B")
	}
	// La misma cuenta de Gmail no puede conectarse a dos usuarios de Pulse.
	state, challenge := gmail.NewState(e.keys.Lookup, b.ID, "s", e.c.Now())
	v, _ := gmail.CheckState(e.keys.Lookup, state, b.ID, "s", e.c.Now())
	code := e.g.Authorize("a@gmail.com", challenge, redirect)
	if _, err := e.s.Connect(context.Background(), b, e.sealer(b), code, v); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("esperaba conflicto, fue %v", err)
	}
	// La conexión de B sigue siendo la suya.
	uid, err := e.st.GmailUserByEmailHash(context.Background(), e.s.EmailHash("B@Gmail.com"))
	if err != nil || uid != b.ID {
		t.Fatalf("%q %v", uid, err)
	}
}

func TestConnectValidaPermisosYPKCE(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	state, challenge := gmail.NewState(e.keys.Lookup, u.ID, "s", e.c.Now())
	v, _ := gmail.CheckState(e.keys.Lookup, state, u.ID, "s", e.c.Now())

	code := e.g.Authorize("a@gmail.com", challenge, redirect)
	if _, err := e.s.Connect(context.Background(), u, e.sealer(u), code, v+"x"); err == nil {
		t.Fatal("un verificador PKCE distinto debe fallar")
	}
	if _, err := e.s.Connect(context.Background(), u, e.sealer(u), code, v); err == nil {
		t.Fatal("un código ya usado debe fallar")
	}
	e.g.OmitScope = true
	code = e.g.Authorize("a@gmail.com", challenge, redirect)
	if _, err := e.s.Connect(context.Background(), u, e.sealer(u), code, v); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("sin el permiso de Gmail debe explicarse: %v", err)
	}
	e.g.OmitScope, e.g.OmitRefresh = false, true
	code = e.g.Authorize("a@gmail.com", challenge, redirect)
	if _, err := e.s.Connect(context.Background(), u, e.sealer(u), code, v); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("sin refresh token debe explicarse: %v", err)
	}
	if _, err := e.st.Gmail(context.Background(), u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("una conexión fallida no debe guardarse")
	}
}

func TestModoSombraApagadoConfirmaSoloLoSeguro(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	ctx := context.Background()
	var comida string
	e.st.DB.QueryRow(`SELECT id FROM categories WHERE user_id = ? AND name = 'Comida'`, u.ID).Scan(&comida)
	if _, err := importer.LearnRule(ctx, e.st, e.keys, u, e.sealer(u), "Pedidosya Groceries", comida); err != nil {
		t.Fatal(err)
	}
	off := false
	if err := e.st.SetGmailOptions(ctx, u.ID, &off, nil); err != nil {
		t.Fatal(err)
	}
	e.g.Deliver("a@gmail.com", fixture(t, "produbanco-consumo")) // con regla → se confirma
	e.g.Deliver("a@gmail.com", fixture(t, "pichincha-consumo"))  // sin regla → se revisa
	r, err := e.s.SyncUser(ctx, u.ID)
	if err != nil || r.Created != 2 || r.Confirmed != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, pending := e.txCount(u.ID); pending != 1 {
		t.Fatalf("pendientes: %d", pending)
	}
}

func TestGeminiOptIn(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	ctx := context.Background()
	e.s.Gemini = &gmail.Gemini{Key: "k", Model: "gemini-2.5-flash", Base: e.g.Endpoints().Gemini, HTTP: gmail.NewHTTPClient()}
	e.g.GeminiReply = `{"is_expense":true,"amount":"9.99","merchant":"TIENDA NUEVA","card_last_digits":"989","date":"2026-10-12 18:24"}`

	// Desactivado (por defecto): no se llama a Gemini.
	e.g.Deliver("a@gmail.com", fixture(t, "pichincha-plantilla-nueva"))
	r, _ := e.s.SyncUser(ctx, u.ID)
	if r.Unrecognized != 1 || e.g.Count("gemini") != 0 {
		t.Fatalf("Gemini se usó sin permiso: %+v", r)
	}

	on := true
	e.st.SetGmailOptions(ctx, u.ID, nil, &on)
	e.g.Deliver("a@gmail.com", []byte(strings.Replace(string(fixture(t, "pichincha-plantilla-nueva")), "plantilla-nueva@", "otra@", 1)))
	r, err := e.s.SyncUser(ctx, u.ID)
	if err != nil || r.Created != 1 || r.Gemini != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	var parser string
	var conf float64
	e.st.DB.QueryRow(`SELECT parser, confidence FROM transactions WHERE user_id = ?`, u.ID).Scan(&parser, &conf)
	if parser != "gemini" || conf > 0.6 {
		t.Fatalf("lo leído por Gemini debe quedar marcado y con confianza baja: %s %v", parser, conf)
	}

	// Una respuesta absurda (fecha lejana) no crea nada.
	e.g.GeminiReply = `{"is_expense":true,"amount":"9.99","merchant":"X","date":"2020-01-01 10:00"}`
	e.g.Deliver("a@gmail.com", []byte(strings.Replace(string(fixture(t, "pichincha-plantilla-nueva")), "plantilla-nueva@", "tercera@", 1)))
	r, _ = e.s.SyncUser(ctx, u.ID)
	if r.Created != 0 || r.Unrecognized != 1 {
		t.Fatalf("%+v", r)
	}
	// Correos de remitentes no bancarios jamás se envían (ni se descargan).
	for _, p := range e.g.GeminiSeen {
		if strings.Contains(p, "titular@example.com") {
			t.Fatal("se envió un email a Gemini")
		}
	}
}

func TestGeminiRecibeTextoRedactado(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.g.GeminiReply = `{"is_expense":false}`
	g := &gmail.Gemini{Key: "k", Model: "m", Base: e.g.Endpoints().Gemini, HTTP: gmail.NewHTTPClient()}
	raw := "From: servicios@tarjetasbancopichincha.com\r\nSubject: Aviso para MATIAS VASCONEZ\r\nDate: Mon, 12 Oct 2026 18:24:51 -0500\r\nMessage-ID: <r@t>\r\n\r\n" +
		"Hola,\r\nMATIAS VASCONEZ\r\nTu cédula 0912345678 y tu correo matias@gmail.com. Tarjeta 4111 1111 1111 1111.\r\n"
	em := parse(t, raw)
	_, why, err := g.Extract(context.Background(), em, time.UTC)
	if err != nil || why == "" {
		t.Fatalf("%q %v", why, err)
	}
	sent := e.g.GeminiSeen[0]
	for _, leak := range []string{"MATIAS VASCONEZ\n", "0912345678", "matias@gmail.com", "4111 1111"} {
		if strings.Contains(sent, leak) {
			t.Errorf("Gemini recibió %q", leak)
		}
	}
	_ = u
}

func TestWatchSeRenueva(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.s.Topic = "projects/pulse-123456/topics/gmail"
	e.connect(u, "a@gmail.com")
	ctx := context.Background()
	e.s.SyncUser(ctx, u.ID)
	e.s.SyncUser(ctx, u.ID)
	if e.g.Count("watch") != 1 {
		t.Fatalf("watch llamado %d veces; debe renovarse solo cerca de caducar", e.g.Count("watch"))
	}
	c, _ := e.st.Gmail(ctx, u.ID)
	if c.WatchExpiresAt == nil || c.HistoryID == 0 {
		t.Fatalf("%+v", c)
	}
	// El watch del falso caduca a 7 días del reloj real; acercamos el reloj de Pulse.
	e.c.Set(c.WatchExpiresAt.Add(-12 * time.Hour))
	e.s.SyncUser(ctx, u.ID)
	if e.g.Count("watch") != 2 {
		t.Fatal("el watch debe renovarse en las últimas 24 h")
	}
	// Desconectar detiene el watch y revoca el token en Google.
	if err := e.s.Disconnect(ctx, u, e.sealer(u)); err != nil {
		t.Fatal(err)
	}
	if e.g.Stopped != 1 || len(e.g.Revoked) != 1 || !strings.HasPrefix(e.g.Revoked[0], "rt-") {
		t.Fatalf("stop=%d revocados=%v", e.g.Stopped, e.g.Revoked)
	}
	if _, err := e.st.Gmail(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("la conexión debe borrarse")
	}
}

func TestAvisoDePubSubAdelantaSincronizacion(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	ctx := context.Background()
	e.s.SyncUser(ctx, u.ID)
	if fresh, _ := e.st.GmailTrigger(ctx, u.ID, 2000); !fresh {
		t.Fatal("un historyId nuevo debe disparar la sincronización")
	}
	if fresh, _ := e.st.GmailTrigger(ctx, u.ID, 1999); fresh {
		t.Fatal("un aviso viejo no debe disparar nada")
	}
	due, _ := e.st.DueGmail(ctx)
	if len(due) != 1 {
		t.Fatal("tras el aviso la sincronización debe estar pendiente")
	}
}

func TestRunProcesaPendientesYAvisos(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.c.Set(time.Now()) // Run usa tickers reales; el reloj de Pulse debe ir al día
	e.connect(u, "a@gmail.com")
	e.g.Deliver("a@gmail.com", fixture(t, "produbanco-consumo"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); e.s.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if total, _ := e.txCount(u.ID); total == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run no procesó el aviso de conexión")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestPlantillaDesconocidaSeRegistraYAvisaUnaVez(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	ctx := context.Background()
	var avisos []string
	e.s.OnTemplate = func(_ context.Context, _ *store.User, parser string) { avisos = append(avisos, parser) }
	e.g.Deliver("a@gmail.com", fixture(t, "pichincha-plantilla-nueva"))
	e.g.Deliver("a@gmail.com", fixture(t, "desconocido")) // remitente que no es banco: no es una plantilla cambiada
	if r, err := e.s.SyncUser(ctx, u.ID); err != nil || r.Unrecognized != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if len(avisos) != 1 || !strings.HasPrefix(avisos[0], "pichincha") {
		t.Fatalf("avisos: %v", avisos)
	}
	alerts, err := e.st.TemplateAlerts(ctx, u.ID, e.sealer(u), e.c.Now().AddDate(0, 0, -1))
	if err != nil || len(alerts) != 1 || alerts[0].Subject == "" {
		t.Fatalf("%+v %v", alerts, err)
	}
	var plain int // el asunto se guarda cifrado
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM template_alerts WHERE instr(subject_enc, ?) > 0`, alerts[0].Subject).Scan(&plain)
	if plain != 0 {
		t.Fatal("asunto en claro")
	}
}

func TestBancoConfiableSeConfirmaSinRegla(t *testing.T) {
	e := newEnv(t)
	u := e.user()
	e.connect(u, "a@gmail.com")
	ctx := context.Background()
	sl := e.sealer(u)
	var cat string
	e.st.DB.QueryRow(`SELECT id FROM categories WHERE user_id = ? AND name = 'Comida'`, u.ID).Scan(&cat)
	acc := &store.Account{ID: auth.NewID(), Name: "Otra", Type: "cash", Currency: "USD"}
	if err := e.st.CreateAccount(ctx, u.ID, acc); err != nil {
		t.Fatal(err)
	}
	// 20 revisiones sin correcciones de ese banco
	for i := range store.MinReviews {
		id := auth.NewID()
		if err := e.st.CreateTransaction(ctx, u.ID, sl, &store.Transaction{ID: id, AccountID: acc.ID, CategoryID: cat, AmountCents: 100, Currency: "USD",
			OccurredAt: e.c.Now(), Kind: "purchase", Source: "gmail", Status: "confirmed", Parser: "pichincha-consumo-v1", MessageHash: []byte{byte(i)}}); err != nil {
			t.Fatal(err)
		}
		e.st.SetReview(ctx, u.ID, id, "ok")
	}
	off := false
	e.st.SetGmailOptions(ctx, u.ID, &off, nil)
	e.g.Deliver("a@gmail.com", fixture(t, "pichincha-consumo")) // sin regla de comercio, pero el banco es confiable
	r, err := e.s.SyncUser(ctx, u.ID)
	if err != nil || r.Created != 1 || r.Confirmed != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

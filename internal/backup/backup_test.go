package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"

	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/store"
)

// fakeS3 guarda lo que recibe y puede fallar las primeras N veces con un código dado.
type fakeS3 struct {
	mu       sync.Mutex
	objects  map[string][]byte
	failN    int
	failWith int
	calls    int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	body, _ := io.ReadAll(r.Body)
	if f.failN > 0 {
		f.failN--
		w.WriteHeader(f.failWith)
		return
	}
	sum := sha256.Sum256(body)
	if r.Method != http.MethodPut || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AK/") ||
		r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) || r.ContentLength != int64(len(body)) {
		w.WriteHeader(400)
		return
	}
	f.objects[r.URL.Path] = body
	w.WriteHeader(200)
}

type fixture struct {
	svc    *Service
	s3     *fakeS3
	st     *store.Store
	keys   *crypto.Keyring
	id     *age.X25519Identity
	clock  *clock.Fake
	sleeps []time.Duration
	alerts []string
	dbPath string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	f := &fixture{s3: &fakeS3{objects: map[string][]byte{}}, clock: clock.NewFake(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))}
	f.dbPath = filepath.Join(dir, "pulse.db")
	d, err := db.Open(f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	f.st = &store.Store{DB: d, Clock: f.clock}
	f.keys, _ = crypto.NewKeyring(bytes.Repeat([]byte{7}, 32))
	f.id, _ = age.GenerateX25519Identity()
	srv := httptest.NewServer(f.s3)
	t.Cleanup(srv.Close)
	ep, _ := url.Parse(srv.URL)
	f.svc = &Service{
		DB: d, Store: f.st, Recipients: []age.Recipient{f.id.Recipient()}, Dir: filepath.Join(dir, "backups"),
		Dest:  &S3{Endpoint: ep, Region: "us-east-1", Bucket: "b", Prefix: "pulse/", AccessKey: "AK", SecretKey: "SK", PathStyle: true, Client: srv.Client()},
		Clock: f.clock, Interval: 24 * time.Hour,
		Sleep: func(_ context.Context, d time.Duration) error { f.sleeps = append(f.sleeps, d); return nil },
		Alert: func(_ context.Context, key, _ string) { f.alerts = append(f.alerts, key) },
	}
	return f
}

// seed crea un usuario con un gasto: el email va en claro en la base, el comercio cifrado con su DEK.
func (f *fixture) seed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	dek := f.keys.NewDEK("u1")
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, role, dek_enc, timezone, created_at) VALUES ('u1', 'ana.secreta@example.com', 'x', 'admin', ?, 'UTC', 'x')`,
		`INSERT INTO accounts (id, user_id, name, type, created_at, updated_at) VALUES ('a1', 'u1', 'Visa', 'credit_card', 'x', 'x')`,
		`INSERT INTO categories (id, user_id, name, created_at, updated_at) VALUES ('c1', 'u1', 'Comida', 'x', 'x')`,
	} {
		var err error
		if strings.Contains(q, "dek_enc") {
			_, err = f.st.DB.Exec(q, dek)
		} else {
			_, err = f.st.DB.Exec(q)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	sl, _ := f.keys.Sealer("u1", dek)
	tx := &store.Transaction{ID: "t1", AccountID: "a1", CategoryID: "c1", AmountCents: 1768, Currency: "USD", OccurredAt: f.clock.Now(), Kind: "purchase", Merchant: "Carl's Jr", Source: "manual", Status: "confirmed"}
	if err := f.st.CreateTransaction(ctx, "u1", sl, tx); err != nil {
		t.Fatal(err)
	}
}

func noPlaintextLeft(t *testing.T, dir string) {
	t.Helper()
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() != LatestName {
			t.Errorf("quedó un archivo de trabajo: %s", p)
		}
		if err == nil && info.IsDir() && strings.HasPrefix(info.Name(), ".trabajo-") {
			t.Errorf("quedó la carpeta de trabajo: %s", p)
		}
		return nil
	})
}

// Ida y vuelta con datos conocidos: respaldar → base vacía → restaurar → comparar.
func TestIdaYVueltaCifrada(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seed(t)
	run, err := f.svc.Run(ctx, "manual", "u1")
	if err != nil {
		t.Fatal(err)
	}
	latest, _ := db.Latest() // el nombre lleva la versión de esquema, que sube con cada migración
	if run.Status != "ok" || run.Attempts != 1 || run.Name != "pulse-20261016T120000Z-v"+strconv.Itoa(latest)+".db.age" || run.SizeBytes == 0 || len(run.SHA256) != 64 {
		t.Fatalf("registro: %+v", run)
	}
	obj := f.s3.objects["/b/pulse/"+run.Name]
	sum := sha256.Sum256(obj)
	if len(obj) == 0 || hex.EncodeToString(sum[:]) != run.SHA256 || int64(len(obj)) != run.SizeBytes {
		t.Fatalf("objeto subido: %d bytes", len(obj))
	}
	// Nada legible: ni la cabecera de SQLite ni el email (que en la base va en claro).
	for _, plain := range []string{"SQLite format 3", "ana.secreta@example.com", "Comida", "Visa"} {
		if bytes.Contains(obj, []byte(plain)) {
			t.Fatalf("el respaldo contiene %q en claro", plain)
		}
	}
	if !bytes.HasPrefix(obj, []byte("age-encryption.org/v1")) {
		t.Fatal("no es un archivo age")
	}
	noPlaintextLeft(t, f.svc.Dir)
	local, _ := os.ReadFile(filepath.Join(f.svc.Dir, LatestName))
	if !bytes.Equal(local, obj) {
		t.Fatal("la copia local para descargar debe ser la misma cifrada")
	}
	// Máquina limpia: se descarga, se descifra con la identidad (fuera del servidor) y se restaura.
	clean := t.TempDir()
	enc := filepath.Join(clean, run.Name)
	os.WriteFile(enc, obj, 0o600)
	plain := filepath.Join(clean, "restaurar.db")
	if err := Decrypt(enc, plain, []age.Identity{f.id}, run.SHA256); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(clean, "pulse.db")
	if err := db.Restore(ctx, plain, target); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	st := &store.Store{DB: d, Clock: f.clock}
	u, err := st.User(ctx, "u1")
	if err != nil || u.Email != "ana.secreta@example.com" {
		t.Fatalf("usuario restaurado: %v %v", u, err)
	}
	sl, err := f.keys.Sealer("u1", u.DEKEnc) // con la misma KEK
	if err != nil {
		t.Fatal(err)
	}
	tx, err := st.Transaction(ctx, "u1", sl, "t1")
	if err != nil || tx.Merchant != "Carl's Jr" || tx.AmountCents != 1768 {
		t.Fatalf("gasto restaurado: %+v %v", tx, err)
	}
	// Con otra KEK los campos sensibles siguen ilegibles: hacen falta los dos secretos.
	other, _ := crypto.NewKeyring(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Sealer("u1", u.DEKEnc); err == nil {
		t.Fatal("otra KEK no debe abrir la DEK")
	}
}

func TestDescifradoRechazaIdentidadAjenaYAlteraciones(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.seed(t)
	run, err := f.svc.Run(ctx, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	enc := filepath.Join(dir, "r.age")
	obj := f.s3.objects["/b/pulse/"+run.Name]
	os.WriteFile(enc, obj, 0o600)
	stranger, _ := age.GenerateX25519Identity()
	if err := Decrypt(enc, filepath.Join(dir, "1.db"), []age.Identity{stranger}, ""); err == nil {
		t.Fatal("una identidad ajena no debe descifrar")
	}
	if err := Decrypt(enc, filepath.Join(dir, "2.db"), []age.Identity{f.id}, strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "huella") {
		t.Fatalf("huella distinta: %v", err)
	}
	bad := append([]byte{}, obj...)
	bad[len(bad)-20] ^= 1
	os.WriteFile(enc, bad, 0o600)
	if err := Decrypt(enc, filepath.Join(dir, "3.db"), []age.Identity{f.id}, ""); err == nil {
		t.Fatal("un archivo alterado no debe descifrarse")
	}
	if _, err := os.Stat(filepath.Join(dir, "3.db")); err == nil {
		t.Fatal("no debe quedar un descifrado parcial")
	}
}

// Destino caído: reintenta con espera creciente; si sigue caído, registra el fallo y avisa.
func TestDestinoCaidoReintentaYAvisa(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.s3.failN, f.s3.failWith = 2, 503
	run, err := f.svc.Run(ctx, "scheduled", "")
	if err != nil || run.Attempts != 3 {
		t.Fatalf("debió recuperarse al 3.er intento: %+v %v", run, err)
	}
	if len(f.sleeps) != 2 || f.sleeps[0] != 5*time.Second || f.sleeps[1] != 10*time.Second {
		t.Fatalf("esperas: %v", f.sleeps)
	}
	f.s3.failN = 100
	f.clock.Advance(time.Hour)
	run, err = f.svc.Run(ctx, "manual", "")
	if err == nil || run.Status != "failed" || run.Attempts != 4 || !strings.Contains(run.Error, "503") {
		t.Fatalf("destino caído: %+v %v", run, err)
	}
	if len(f.alerts) != 1 || !strings.HasPrefix(f.alerts[0], "backup-failed:") {
		t.Fatalf("avisos: %v", f.alerts)
	}
	noPlaintextLeft(t, f.svc.Dir)
	st, _ := f.svc.Status(ctx)
	if st.State != "failing" || len(st.Runs) != 2 || st.LastOK == nil {
		t.Fatalf("estado: %+v", st)
	}
	// Credenciales malas (403): no se insiste.
	f.s3.failN, f.s3.failWith = 1, 403
	f.sleeps = nil
	run, _ = f.svc.Run(ctx, "manual", "")
	if run.Attempts != 1 || len(f.sleeps) != 0 {
		t.Fatalf("un 403 no se reintenta: %+v", run)
	}
}

func TestEstadoProgramacionYConcurrencia(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if st, _ := f.svc.Status(ctx); st.State != "pending" || st.Download {
		t.Fatalf("sin respaldos: %+v", st)
	}
	if st, _ := (*Service)(nil).Status(ctx); st.State != "off" {
		t.Fatal("sin configurar debe estar apagado")
	}
	f.svc.Tick(ctx) // nunca hubo uno: toca
	if f.s3.calls != 1 {
		t.Fatalf("subidas: %d", f.s3.calls)
	}
	f.clock.Advance(23 * time.Hour)
	f.svc.Tick(ctx)
	if f.s3.calls != 1 {
		t.Fatal("antes del intervalo no toca")
	}
	f.clock.Advance(time.Hour)
	f.svc.Tick(ctx)
	if f.s3.calls != 2 {
		t.Fatal("al cumplirse el intervalo toca")
	}
	if st, _ := f.svc.Status(ctx); st.State != "ok" || !st.Download || st.LastOK == nil {
		t.Fatalf("estado: %+v", st)
	}
	// Sin respaldos correctos por más de dos intervalos: "stale" y un aviso por día.
	f.svc.Interval = 0 // solo manual
	f.clock.Advance(9 * 24 * time.Hour)
	f.svc.Tick(ctx)
	f.svc.Tick(ctx)
	if st, _ := f.svc.Status(ctx); st.State != "stale" {
		t.Fatalf("estado: %+v", st)
	}
	if len(f.alerts) != 2 || f.alerts[0] != f.alerts[1] || !strings.HasPrefix(f.alerts[0], "backup-stale:") {
		t.Fatalf("avisos (la clave del día evita repetirlo): %v", f.alerts)
	}
	// Dos respaldos a la vez: el segundo se rechaza.
	f.svc.mu.Lock()
	f.svc.running = true
	f.svc.mu.Unlock()
	if _, err := f.svc.Run(ctx, "manual", ""); err != ErrBusy {
		t.Fatalf("concurrencia: %v", err)
	}
}

func TestWebDAVYDestinatarios(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, body = r, nil
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL + "/remote.php/dav/files/ana/pulse-backups/")
	w := &WebDAV{URL: u, User: "ana", Password: "app-pass"}
	if err := w.Put(context.Background(), "x.db.age", bytes.NewReader([]byte("cifrado")), 7, "abc"); err != nil {
		t.Fatal(err)
	}
	user, pass, ok := got.BasicAuth()
	if got.URL.Path != "/remote.php/dav/files/ana/pulse-backups/x.db.age" || !ok || user != "ana" || pass != "app-pass" || string(body) != "cifrado" || got.Header.Get("OC-Checksum") != "SHA256:abc" {
		t.Fatalf("petición WebDAV: %s %v", got.URL.Path, got.Header)
	}
	if strings.Contains(w.Describe(), "app-pass") {
		t.Fatal("la descripción no debe mostrar la contraseña")
	}
	if _, err := ParseRecipients(""); err == nil {
		t.Fatal("sin destinatarios")
	}
	if _, err := ParseRecipients("age1nope"); err == nil {
		t.Fatal("clave inválida")
	}
	a, _ := age.GenerateX25519Identity()
	b, _ := age.GenerateX25519Identity()
	if rs, err := ParseRecipients(a.Recipient().String() + ", " + b.Recipient().String()); err != nil || len(rs) != 2 {
		t.Fatalf("dos destinatarios: %v", err)
	}
	// Las redirecciones no se siguen: la credencial no viaja a otro sitio.
	red := httptest.NewServer(http.RedirectHandler("https://otro.example/", http.StatusTemporaryRedirect))
	defer red.Close()
	ru, _ := url.Parse(red.URL + "/")
	if err := (&WebDAV{URL: ru, User: "a", Password: "b"}).Put(context.Background(), "x", bytes.NewReader(nil), 0, ""); err == nil {
		t.Fatal("debió rechazar la redirección")
	}
}

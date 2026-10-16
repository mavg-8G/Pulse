package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func sumOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestLocalGuardaYRota(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backup", "pulse")
	l := &Local{Dir: dir, Keep: 2}
	for i := 1; i <= 3; i++ {
		body := []byte(fmt.Sprintf("cifrado %d", i))
		name := fmt.Sprintf("pulse-2026101%dT000000Z-v13.db.age", i)
		if err := l.Put(context.Background(), name, bytes.NewReader(body), int64(len(body)), sumOf(body)); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 2 || !strings.HasSuffix(files[0], "pulse-20261012T000000Z-v13.db.age") {
		t.Fatalf("debían quedar los 2 más nuevos: %v", files)
	}
	if st, _ := os.Stat(files[1]); st.Mode().Perm() != 0o600 {
		t.Errorf("permisos %v", st.Mode().Perm())
	}
	if err := l.Put(context.Background(), "../fuera.age", bytes.NewReader(nil), 0, sumOf(nil)); err == nil {
		t.Error("aceptó un nombre con ruta")
	}
	if err := l.Put(context.Background(), "pulse-x.db.age", strings.NewReader("abc"), 3, sumOf([]byte("otro"))); err == nil {
		t.Error("aceptó una copia con huella distinta")
	}
}

type flakyDest struct {
	fail  []error
	calls int
	got   []byte
}

func (f *flakyDest) Describe() string { return "falso" }
func (f *flakyDest) Put(_ context.Context, _ string, body io.ReadSeeker, _ int64, _ string) error {
	f.calls++
	if len(f.fail) > 0 {
		err := f.fail[0]
		f.fail = f.fail[1:]
		return err
	}
	f.got, _ = io.ReadAll(body)
	return nil
}

func TestMultiReintentaSoloLosQueFaltan(t *testing.T) {
	ok, bad := &flakyDest{}, &flakyDest{fail: []error{errors.New("caído")}}
	m := &Multi{Dests: []Destination{ok, bad}}
	body := bytes.NewReader([]byte("cifrado"))
	err := m.Put(context.Background(), "a.age", body, 7, "")
	var perm *PermanentError
	if err == nil || errors.As(err, &perm) {
		t.Fatalf("un fallo temporal debe reintentarse: %v", err)
	}
	if err := m.Put(context.Background(), "a.age", body, 7, ""); err != nil {
		t.Fatal(err)
	}
	if ok.calls != 1 || bad.calls != 2 || string(bad.got) != "cifrado" {
		t.Fatalf("llamadas ok=%d bad=%d got=%q", ok.calls, bad.calls, bad.got)
	}
	perma := &flakyDest{fail: []error{&PermanentError{errors.New("sin permiso")}}}
	m = &Multi{Dests: []Destination{&flakyDest{}, perma}}
	if err := m.Put(context.Background(), "b.age", body, 7, ""); !errors.As(err, &perm) {
		t.Fatalf("debía ser permanente: %v", err)
	}
}

// fakeDrive imita lo justo de OAuth y Drive v3.
type fakeDrive struct {
	mu       sync.Mutex
	srv      *httptest.Server
	files    map[string]*fakeFile
	next     int
	tokens   int
	revoked  bool
	corrupt  bool
	uploads  int
	failPuts int
}

type fakeFile struct {
	id, name, mime, parent string
	data                   []byte
	trashed                bool
}

func newFakeDrive(t *testing.T) *fakeDrive {
	f := &fakeDrive{files: map[string]*fakeFile{}}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDrive) ep() GDriveEndpoints {
	return GDriveEndpoints{Auth: f.srv.URL + "/auth", Token: f.srv.URL + "/token", API: f.srv.URL + "/drive/v3", Upload: f.srv.URL + "/upload/drive/v3"}
}

func (f *fakeDrive) add(name, mime, parent string, data []byte) *fakeFile {
	f.next++
	ff := &fakeFile{id: fmt.Sprintf("id%d", f.next), name: name, mime: mime, parent: parent, data: data}
	f.files[ff.id] = ff
	return ff
}

func (f *fakeDrive) json(ff *fakeFile) map[string]any {
	return map[string]any{"id": ff.id, "name": ff.name, "size": fmt.Sprint(len(ff.data)), "sha256Checksum": sumOf(ff.data)}
}

func (f *fakeDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/token" {
		r.ParseForm()
		if f.revoked || r.Form.Get("refresh_token") != "refresco" || r.Form.Get("client_secret") != "secreto" {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		f.tokens++
		io.WriteString(w, `{"access_token":"acceso","expires_in":3600}`)
		return
	}
	// La URL de sesión de una subida reanudable ya va autenticada (como en Google).
	if r.Header.Get("Authorization") != "Bearer acceso" && !strings.HasPrefix(r.URL.Path, "/session/") {
		w.WriteHeader(401)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/drive/v3/files":
		q := r.URL.Query().Get("q")
		var out []map[string]any
		for _, ff := range f.files {
			if ff.trashed || !strings.Contains(q, "'"+ff.parent+"' in parents") {
				continue
			}
			if strings.Contains(q, "name = '") && !strings.Contains(q, "name = '"+ff.name+"'") {
				continue
			}
			if strings.Contains(q, "mimeType = 'application/vnd.google-apps.folder'") != (ff.mime == folderMime) && strings.Contains(q, "mimeType") {
				continue
			}
			out = append(out, f.json(ff))
		}
		json.NewEncoder(w).Encode(map[string]any{"files": out})
	case r.Method == http.MethodPost && r.URL.Path == "/drive/v3/files":
		var m struct {
			Name, MimeType string
			Parents        []string
		}
		json.NewDecoder(r.Body).Decode(&m)
		json.NewEncoder(w).Encode(f.json(f.add(m.Name, m.MimeType, m.Parents[0], nil)))
	case r.Method == http.MethodPost && r.URL.Path == "/upload/drive/v3/files":
		var m struct {
			Name    string
			Parents []string
		}
		json.NewDecoder(r.Body).Decode(&m)
		ff := f.add(m.Name, "application/octet-stream", m.Parents[0], nil)
		ff.trashed = true // hasta que llegue el contenido
		w.Header().Set("Location", f.srv.URL+"/session/"+ff.id)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/session/"):
		ff := f.files[strings.TrimPrefix(r.URL.Path, "/session/")]
		if f.failPuts > 0 {
			f.failPuts--
			w.WriteHeader(503)
			return
		}
		ff.data, _ = io.ReadAll(r.Body)
		if f.corrupt {
			ff.data = append(ff.data, 'x')
		}
		ff.trashed = false
		f.uploads++
		json.NewEncoder(w).Encode(map[string]any{"id": ff.id})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/drive/v3/files/"):
		json.NewEncoder(w).Encode(f.json(f.files[strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")]))
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/drive/v3/files/"):
		f.files[strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")].trashed = true
		io.WriteString(w, `{}`)
	default:
		w.WriteHeader(404)
	}
}

func (f *fakeDrive) live(parent string) []string {
	var names []string
	for _, ff := range f.files {
		if !ff.trashed && ff.parent == parent {
			names = append(names, ff.name)
		}
	}
	return names
}

func TestGoogleDriveCreaCarpetasSubeYRota(t *testing.T) {
	fd := newFakeDrive(t)
	backups := fd.add("Backups", folderMime, "root", nil) // la carpeta Backups ya existe; pulse no
	g := &GDrive{ClientID: "cliente", ClientSecret: "secreto", RefreshToken: "refresco", Folder: "Backups/pulse", Keep: 2, EP: fd.ep()}
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		body := []byte(fmt.Sprintf("cifrado %d", i))
		name := fmt.Sprintf("pulse-2026101%dT000000Z-v13.db.age", i)
		if err := g.Put(ctx, name, bytes.NewReader(body), int64(len(body)), sumOf(body)); err != nil {
			t.Fatal(err)
		}
	}
	folders := fd.live(backups.id)
	if len(folders) != 1 || folders[0] != "pulse" {
		t.Fatalf("carpetas bajo Backups: %v", folders)
	}
	if len(fd.live("root")) != 1 {
		t.Fatalf("no debía crear otra carpeta Backups: %v", fd.live("root"))
	}
	pulse := g.folderID
	if got := fd.live(pulse); len(got) != 2 || strings.Join(got, ",") == "" || contains(got, "pulse-20261011T000000Z-v13.db.age") {
		t.Fatalf("en Backups/pulse debían quedar los 2 más nuevos: %v", got)
	}
	if fd.tokens != 1 {
		t.Errorf("renovó el token %d veces; debía reutilizarlo", fd.tokens)
	}
	// Reintento de un respaldo que ya llegó: no se sube otra vez.
	body := []byte("cifrado 3")
	if err := g.Put(ctx, "pulse-20261013T000000Z-v13.db.age", bytes.NewReader(body), int64(len(body)), sumOf(body)); err != nil || fd.uploads != 3 {
		t.Fatalf("duplicó la subida: %v uploads=%d", err, fd.uploads)
	}
	if strings.Contains(g.Describe(), "secreto") || strings.Contains(g.Describe(), "refresco") {
		t.Error("la descripción no debe mostrar credenciales")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestGoogleDriveErrores(t *testing.T) {
	ctx := context.Background()
	body := []byte("cifrado")
	var perm *PermanentError

	fd := newFakeDrive(t)
	fd.revoked = true
	g := &GDrive{ClientID: "c", ClientSecret: "secreto", RefreshToken: "refresco", Folder: "Backups/pulse", EP: fd.ep()}
	err := g.Put(ctx, "pulse-a.db.age", bytes.NewReader(body), 7, sumOf(body))
	if !errors.As(err, &perm) || !strings.Contains(err.Error(), "gdrive-auth") {
		t.Fatalf("token revocado debe ser permanente y decir cómo arreglarlo: %v", err)
	}

	fd = newFakeDrive(t)
	fd.corrupt = true
	g = &GDrive{ClientID: "c", ClientSecret: "secreto", RefreshToken: "refresco", Folder: "Backups/pulse", EP: fd.ep()}
	if err := g.Put(ctx, "pulse-a.db.age", bytes.NewReader(body), 7, sumOf(body)); err == nil || !strings.Contains(err.Error(), "distinto") {
		t.Fatalf("no detectó que Drive guardó otra cosa: %v", err)
	}

	fd = newFakeDrive(t)
	fd.failPuts = 1
	g = &GDrive{ClientID: "c", ClientSecret: "secreto", RefreshToken: "refresco", Folder: "Backups/pulse", EP: fd.ep()}
	if err := g.Put(ctx, "pulse-a.db.age", bytes.NewReader(body), 7, sumOf(body)); err == nil || errors.As(err, &perm) {
		t.Fatalf("un 503 debe poder reintentarse: %v", err)
	}
	if err := g.Put(ctx, "pulse-a.db.age", bytes.NewReader(body), 7, sumOf(body)); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleDriveAutorizacion(t *testing.T) {
	var gotForm url.Values
	tok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotForm = r.Form
		io.WriteString(w, `{"refresh_token":"refresco-nuevo","scope":"`+GDriveScope+`"}`)
	}))
	defer tok.Close()
	ep := GDriveEndpoints{Auth: "https://accounts.example/auth", Token: tok.URL}
	done := make(chan error, 1)
	var refresh string
	go func() {
		var err error
		refresh, err = GDriveAuthorize(context.Background(), ep, "cliente", "secreto", "127.0.0.1:0", func(u string) {
			au, _ := url.Parse(u)
			q := au.Query()
			if q.Get("scope") != GDriveScope || q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" {
				done <- fmt.Errorf("URL de autorización incompleta: %s", u)
				return
			}
			// Una respuesta con state falso se rechaza.
			go func() {
				if resp, err := http.Get(q.Get("redirect_uri") + "?code=x&state=falso"); err == nil {
					resp.Body.Close()
				}
			}()
		})
		done <- err
	}()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("debía rechazar un state distinto: %v", err)
	}

	go func() {
		var err error
		refresh, err = GDriveAuthorize(context.Background(), ep, "cliente", "secreto", "127.0.0.1:0", func(u string) {
			au, _ := url.Parse(u)
			q := au.Query()
			go func() {
				resp, err := http.Get(q.Get("redirect_uri") + "?code=codigo&state=" + url.QueryEscape(q.Get("state")))
				if err == nil {
					resp.Body.Close()
				}
			}()
		})
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if refresh != "refresco-nuevo" || gotForm.Get("code") != "codigo" || gotForm.Get("code_verifier") == "" || !strings.HasPrefix(gotForm.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("canje incorrecto: %q %v", refresh, gotForm)
	}
}

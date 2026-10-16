package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------- Google Drive ----------------

// GDriveScope solo deja ver y tocar los archivos que creó Pulse: no puede leer el resto del Drive.
const GDriveScope = "https://www.googleapis.com/auth/drive.file"

const folderMime = "application/vnd.google-apps.folder"

// GDriveEndpoints de Google; los tests apuntan a un servidor falso.
type GDriveEndpoints struct{ Auth, Token, API, Upload string }

func GoogleDriveEndpoints() GDriveEndpoints {
	return GDriveEndpoints{
		Auth:   "https://accounts.google.com/o/oauth2/v2/auth",
		Token:  "https://oauth2.googleapis.com/token",
		API:    "https://www.googleapis.com/drive/v3",
		Upload: "https://www.googleapis.com/upload/drive/v3",
	}
}

// GDrive sube a una carpeta del Drive personal (p. ej. "Backups/pulse") con un refresh token OAuth de la
// persona (ver `pulse gdrive-auth`). Las carpetas que falten las crea; los respaldos que pasen de Keep van a la
// papelera de Drive (se pueden recuperar 30 días).
type GDrive struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	Folder       string // ruta desde la raíz de Mi unidad, separada por "/"
	Keep         int    // 0 = conservar todos
	EP           GDriveEndpoints
	Client       *http.Client

	mu       sync.Mutex
	access   string
	expires  time.Time
	folderID string
}

func (g *GDrive) Describe() string { return "Google Drive · " + g.Folder }

func (g *GDrive) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return defaultClient
}

// token devuelve un access token vigente, renovándolo con el refresh token.
func (g *GDrive) token(ctx context.Context) (string, error) {
	if g.access != "" && time.Now().Before(g.expires) {
		return g.access, nil
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {g.RefreshToken},
		"client_id": {g.ClientID}, "client_secret": {g.ClientSecret}}
	var out struct {
		Access    string `json:"access_token"`
		ExpiresIn int    `json:"expires_in"`
		Error     string `json:"error"`
	}
	if err := postForm(ctx, g.client(), g.EP.Token, form, &out); err != nil {
		if out.Error == "invalid_grant" || out.Error == "invalid_client" || out.Error == "unauthorized_client" {
			return "", &PermanentError{fmt.Errorf("Google rechazó el permiso de Drive (%s): vuelve a ejecutar pulse gdrive-auth", out.Error)}
		}
		return "", err
	}
	g.access, g.expires = out.Access, time.Now().Add(time.Duration(out.ExpiresIn)*time.Second-time.Minute)
	return g.access, nil
}

func postForm(ctx context.Context, c *http.Client, u string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return &PermanentError{err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	json.Unmarshal(body, out)
	if resp.StatusCode/100 != 2 {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return statusError(resp)
	}
	return nil
}

// do llama a la API de Drive con JSON. 403 por cuota/limitación se reintenta; el resto de 4xx no.
func (g *GDrive) do(ctx context.Context, method, u string, in, out any) (*http.Response, error) {
	tok, err := g.token(ctx)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, &PermanentError{err}
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	return g.send(req, out)
}

func (g *GDrive) send(req *http.Request, out any) (*http.Response, error) {
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == 401 {
			g.access = "" // el siguiente intento renueva el token
			return resp, fmt.Errorf("Google Drive respondió 401")
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		err := statusError(resp)
		if resp.StatusCode == 403 && (bytes.Contains(body, []byte("ateLimitExceeded")) || bytes.Contains(body, []byte("userRateLimit"))) {
			err = errors.Unwrap(err)
		}
		return resp, err
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return resp, fmt.Errorf("respuesta inesperada de Google Drive: %w", err)
		}
	}
	return resp, nil
}

type driveFile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   string `json:"size"`
	SHA256 string `json:"sha256Checksum"`
}

// qEscape escapa un literal para el parámetro q de Drive.
func qEscape(s string) string { return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) }

func (g *GDrive) list(ctx context.Context, q string) ([]driveFile, error) {
	var files []driveFile
	page := ""
	for {
		v := url.Values{"q": {q + " and trashed = false"}, "spaces": {"drive"}, "pageSize": {"1000"},
			"fields": {"nextPageToken,files(id,name,size,sha256Checksum)"}}
		if page != "" {
			v.Set("pageToken", page)
		}
		var out struct {
			Next  string      `json:"nextPageToken"`
			Files []driveFile `json:"files"`
		}
		if _, err := g.do(ctx, http.MethodGet, g.EP.API+"/files?"+v.Encode(), nil, &out); err != nil {
			return nil, err
		}
		files = append(files, out.Files...)
		if page = out.Next; page == "" {
			return files, nil
		}
	}
}

// folder busca (o crea) cada tramo de la ruta y recuerda el id de la carpeta final.
func (g *GDrive) folder(ctx context.Context) (string, error) {
	if g.folderID != "" {
		return g.folderID, nil
	}
	parent := "root"
	for _, name := range strings.Split(strings.Trim(g.Folder, "/"), "/") {
		found, err := g.list(ctx, fmt.Sprintf("name = '%s' and mimeType = '%s' and '%s' in parents", qEscape(name), folderMime, parent))
		if err != nil {
			return "", fmt.Errorf("buscando la carpeta %q: %w", name, err)
		}
		if len(found) > 0 {
			parent = found[0].ID
			continue
		}
		var created driveFile
		meta := map[string]any{"name": name, "mimeType": folderMime, "parents": []string{parent}}
		if _, err := g.do(ctx, http.MethodPost, g.EP.API+"/files?fields=id", meta, &created); err != nil {
			return "", fmt.Errorf("creando la carpeta %q: %w", name, err)
		}
		parent = created.ID
	}
	g.folderID = parent
	return parent, nil
}

func (g *GDrive) Put(ctx context.Context, name string, body io.ReadSeeker, size int64, hexSum string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	err := g.put(ctx, name, body, size, hexSum)
	if err != nil {
		g.folderID = "" // por si borraron la carpeta: el próximo intento la vuelve a buscar
	}
	return err
}

func (g *GDrive) put(ctx context.Context, name string, body io.ReadSeeker, size int64, hexSum string) error {
	parent, err := g.folder(ctx)
	if err != nil {
		return err
	}
	// Si un intento anterior sí llegó (se perdió la respuesta), no se sube dos veces.
	existing, err := g.list(ctx, fmt.Sprintf("name = '%s' and '%s' in parents", qEscape(name), parent))
	if err != nil {
		return err
	}
	for _, f := range existing {
		if strings.EqualFold(f.SHA256, hexSum) {
			return g.prune(ctx, parent)
		}
	}
	// Subida reanudable: sirve para cualquier tamaño (la multipart se limita a 5 MB).
	meta := map[string]any{"name": name, "parents": []string{parent}, "mimeType": "application/octet-stream"}
	tok, err := g.token(ctx)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(meta)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.EP.Upload+"/files?uploadType=resumable", bytes.NewReader(b))
	if err != nil {
		return &PermanentError{err}
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprint(size))
	resp, err := g.send(req, nil)
	if err != nil {
		return err
	}
	session := resp.Header.Get("Location")
	if su, err := url.Parse(session); session == "" || err != nil || su.Scheme != "https" && !strings.HasPrefix(g.EP.Upload, "http://") {
		return errors.New("Google Drive no devolvió una URL de subida válida")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPut, session, body)
	if err != nil {
		return &PermanentError{err}
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	var up driveFile
	if _, err := g.send(req, &up); err != nil {
		return err
	}
	// Drive calcula el SHA-256 de lo que guardó: si no coincide, el archivo no sirve.
	var got driveFile
	if _, err := g.do(ctx, http.MethodGet, g.EP.API+"/files/"+url.PathEscape(up.ID)+"?fields=id,size,sha256Checksum", nil, &got); err != nil {
		return err
	}
	if got.Size != fmt.Sprint(size) || got.SHA256 != "" && !strings.EqualFold(got.SHA256, hexSum) {
		return fmt.Errorf("Google Drive guardó un archivo distinto (tamaño %s, sha256 %s)", got.Size, got.SHA256)
	}
	return g.prune(ctx, parent)
}

// prune manda a la papelera los respaldos más viejos de la carpeta. Solo ve archivos creados por Pulse.
func (g *GDrive) prune(ctx context.Context, parent string) error {
	if g.Keep <= 0 {
		return nil
	}
	files, err := g.list(ctx, fmt.Sprintf("'%s' in parents and name contains 'pulse-' and mimeType != '%s'", parent, folderMime))
	if err != nil {
		return err
	}
	var ours []driveFile
	for _, f := range files {
		if strings.HasPrefix(f.Name, "pulse-") && strings.HasSuffix(f.Name, ".db.age") {
			ours = append(ours, f)
		}
	}
	sort.Slice(ours, func(i, j int) bool { return ours[i].Name < ours[j].Name })
	for len(ours) > g.Keep {
		if _, err := g.do(ctx, http.MethodPatch, g.EP.API+"/files/"+url.PathEscape(ours[0].ID)+"?fields=id", map[string]any{"trashed": true}, nil); err != nil {
			return fmt.Errorf("enviando a la papelera %s: %w", ours[0].Name, err)
		}
		ours = ours[1:]
	}
	return nil
}

// ---------------- Autorización inicial (pulse gdrive-auth) ----------------

// GDriveAuthorize hace el flujo OAuth de "app de escritorio" con redirección a 127.0.0.1 y PKCE, y devuelve
// el refresh token. listen es la dirección local donde esperar a Google (p. ej. 127.0.0.1:53682); la URL de
// redirección siempre usa 127.0.0.1 y ese puerto, así que también funciona a través de un túnel SSH.
// show recibe la URL que hay que abrir en el navegador.
func GDriveAuthorize(ctx context.Context, ep GDriveEndpoints, clientID, clientSecret, listen string, show func(authURL string)) (string, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return "", err
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	redirect := "http://127.0.0.1:" + port + "/"
	state, verifier := randToken(), randToken()
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {GDriveScope},
		"access_type": {"offline"}, "prompt": {"consent"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	show(ep.Auth + "?" + q.Encode())

	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query()
		if v.Get("state") == "" && v.Get("code") == "" && v.Get("error") == "" {
			http.NotFound(w, r) // favicon y similares
			return
		}
		res := result{code: v.Get("code")}
		switch {
		case v.Get("state") != state:
			res.err = errors.New("la respuesta de Google no corresponde a esta autorización (state distinto)")
		case v.Get("error") != "":
			res.err = fmt.Errorf("Google no dio el permiso: %s", v.Get("error"))
		case res.code == "":
			res.err = errors.New("Google no devolvió el código de autorización")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if res.err != nil {
			w.WriteHeader(400)
			fmt.Fprintln(w, "Pulse: "+res.err.Error())
		} else {
			fmt.Fprintln(w, "Pulse: listo, ya puedes cerrar esta pestaña y volver a la terminal.")
		}
		select {
		case ch <- res:
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()
	var res result
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res = <-ch:
	}
	if res.err != nil {
		return "", res.err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {res.code}, "redirect_uri": {redirect},
		"client_id": {clientID}, "client_secret": {clientSecret}, "code_verifier": {verifier}}
	var out struct {
		Refresh string `json:"refresh_token"`
		Scope   string `json:"scope"`
		Error   string `json:"error"`
	}
	c := &http.Client{Timeout: 20 * time.Second}
	if err := postForm(ctx, c, ep.Token, form, &out); err != nil {
		return "", fmt.Errorf("canjeando el código: %w", err)
	}
	if out.Refresh == "" {
		return "", errors.New("Google no entregó un refresh token; quita el acceso de Pulse en myaccount.google.com/permissions y repite")
	}
	if !strings.Contains(out.Scope, GDriveScope) {
		return "", errors.New("no se concedió el permiso de Google Drive; repite y marca la casilla de Drive")
	}
	return out.Refresh, nil
}

func randToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

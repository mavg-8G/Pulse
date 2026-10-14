// Package gmailtest es un Google falso (OAuth, Gmail, certificados de Pub/Sub y Gemini)
// para probar la integración sin red. Implementa lo justo, pero valida como Google:
// PKCE, client_id/secret, tokens, consulta por remitente.
package gmailtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pulse/internal/gmail"
)

const (
	ClientID     = "pulse-test.apps.googleusercontent.com"
	ClientSecret = "secreto-de-prueba"
	Kid          = "clave-1"
)

type Message struct {
	ID   string
	From string
	Raw  []byte
}

type account struct {
	email    string
	messages []Message
	history  uint64
}

type Server struct {
	*httptest.Server
	mu       sync.Mutex
	accounts map[string]*account // por email
	codes    map[string]codeGrant
	refresh  map[string]string // refresh token → email
	access   map[string]string // access token → email
	Key      *rsa.PrivateKey

	// Inyección de fallos y contadores
	Fail        map[string][]int // ruta ("list", "raw", "token", "watch", "profile") → estados a devolver en orden
	RetryAfter  string
	Calls       map[string]int
	RawFetched  []string // ids descargados (para comprobar que solo se bajan los del banco)
	Queries     []string
	Revoked     []string
	Stopped     int
	GeminiReply string // JSON que "responde" Gemini
	GeminiSeen  []string
	OmitScope   bool
	OmitRefresh bool
	ExpiresIn   int
}

type codeGrant struct {
	email, challenge, redirect string
}

func New() *Server {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	f := &Server{accounts: map[string]*account{}, codes: map[string]codeGrant{}, refresh: map[string]string{}, access: map[string]string{},
		Key: key, Fail: map[string][]int{}, Calls: map[string]int{}, ExpiresIn: 3599}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *Server) Endpoints() gmail.Endpoints { return gmail.FakeEndpoints(f.URL) }

// AddAccount crea un buzón y devuelve un código de autorización para él (como si la persona
// hubiera aceptado en Google). challenge es el code_challenge PKCE.
func (f *Server) Authorize(email, challenge, redirect string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensure(email)
	code := "code-" + randID()
	f.codes[code] = codeGrant{email, challenge, redirect}
	return code
}

func (f *Server) ensure(email string) *account {
	a, ok := f.accounts[email]
	if !ok {
		a = &account{email: email, history: 1000}
		f.accounts[email] = a
	}
	return a
}

// Deliver mete un correo en el buzón (raw = .eml completo).
func (f *Server) Deliver(email string, raw []byte) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.ensure(email)
	from := ""
	if m, err := mail.ReadMessage(strings.NewReader(string(raw))); err == nil {
		if addr, err := mail.ParseAddress(m.Header.Get("From")); err == nil {
			from = strings.ToLower(addr.Address)
		}
	}
	id := fmt.Sprintf("%x", len(a.messages)+1) + randID()[:8]
	a.messages = append([]Message{{ID: id, From: from, Raw: raw}}, a.messages...) // Gmail: más nuevo primero
	a.history++
	return id
}

// RevokeAll simula que la persona quitó el acceso desde su cuenta de Google.
func (f *Server) RevokeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = map[string]string{}
	f.access = map[string]string{}
}

// ExpireAccess invalida los access tokens (pero no los refresh).
func (f *Server) ExpireAccess() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.access = map[string]string{}
}

func (f *Server) Count(k string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls[k]
}

func (f *Server) failNext(k string) (int, bool) {
	q := f.Fail[k]
	if len(q) == 0 {
		return 0, false
	}
	f.Fail[k] = q[1:]
	return q[0], true
}

var reFrom = regexp.MustCompile(`from:\(([^)]*)\)`)

func (f *Server) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	key := map[string]string{"/token": "token", "/revoke": "revoke", "/certs": "certs", "/o/oauth2/v2/auth": "auth"}[path]
	switch {
	case strings.HasSuffix(path, "/profile"):
		key = "profile"
	case strings.HasSuffix(path, "/messages"):
		key = "list"
	case strings.Contains(path, "/messages/"):
		key = "raw"
	case strings.HasSuffix(path, "/watch"):
		key = "watch"
	case strings.HasSuffix(path, "/stop"):
		key = "stop"
	case strings.HasPrefix(path, "/gemini/"):
		key = "gemini"
	}
	f.Calls[key]++
	if st, ok := f.failNext(key); ok {
		if f.RetryAfter != "" {
			w.Header().Set("Retry-After", f.RetryAfter)
		}
		if key == "token" && st == 400 {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		writeJSON(w, st, map[string]any{"error": map[string]any{"code": st}})
		return
	}
	switch key {
	case "auth":
		f.auth(w, r)
	case "token":
		f.token(w, r)
	case "revoke":
		r.ParseForm()
		t := r.PostForm.Get("token")
		f.Revoked = append(f.Revoked, t)
		delete(f.refresh, t)
		delete(f.access, t)
		w.WriteHeader(200)
	case "certs":
		w.Header().Set("Cache-Control", "public, max-age=3600")
		writeJSON(w, 200, map[string]any{"keys": []map[string]string{{"kid": Kid, "kty": "RSA", "alg": "RS256", "use": "sig",
			"n": b64(f.Key.N.Bytes()), "e": b64(big.NewInt(int64(f.Key.E)).Bytes())}}})
	case "gemini":
		var in struct {
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if r.Header.Get("x-goog-api-key") == "" || len(in.Contents) == 0 {
			writeJSON(w, 403, map[string]string{"error": "sin clave"})
			return
		}
		f.GeminiSeen = append(f.GeminiSeen, in.Contents[0].Parts[0].Text)
		writeJSON(w, 200, map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]string{"text": f.GeminiReply}}}}}})
	default:
		a := f.authorized(r)
		if a == nil {
			writeJSON(w, 401, map[string]any{"error": map[string]any{"code": 401}})
			return
		}
		f.gmail(w, r, key, a)
	}
}

func (f *Server) auth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != ClientID || q.Get("code_challenge_method") != "S256" || q.Get("scope") != gmail.Scope || q.Get("access_type") != "offline" {
		http.Error(w, "petición OAuth inválida", 400)
		return
	}
	email := q.Get("login_hint")
	if email == "" {
		email = "persona@gmail.com"
	}
	f.ensure(email)
	code := "code-" + randID()
	f.codes[code] = codeGrant{email, q.Get("code_challenge"), q.Get("redirect_uri")}
	u, _ := url.Parse(q.Get("redirect_uri"))
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	v.Set("scope", gmail.Scope)
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (f *Server) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	p := r.PostForm
	if p.Get("client_id") != ClientID || p.Get("client_secret") != ClientSecret {
		writeJSON(w, 401, map[string]string{"error": "invalid_client"})
		return
	}
	switch p.Get("grant_type") {
	case "authorization_code":
		g, ok := f.codes[p.Get("code")]
		delete(f.codes, p.Get("code")) // un solo uso
		sum := sha256.Sum256([]byte(p.Get("code_verifier")))
		if !ok || b64(sum[:]) != g.challenge || p.Get("redirect_uri") != g.redirect {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		rt, at := "rt-"+randID(), "at-"+randID()
		f.refresh[rt], f.access[at] = g.email, g.email
		out := map[string]any{"access_token": at, "expires_in": f.ExpiresIn, "token_type": "Bearer", "scope": gmail.Scope}
		if f.OmitScope {
			out["scope"] = "openid"
		}
		if !f.OmitRefresh {
			out["refresh_token"] = rt
		}
		writeJSON(w, 200, out)
	case "refresh_token":
		email, ok := f.refresh[p.Get("refresh_token")]
		if !ok {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		at := "at-" + randID()
		f.access[at] = email
		writeJSON(w, 200, map[string]any{"access_token": at, "expires_in": f.ExpiresIn, "token_type": "Bearer", "scope": gmail.Scope})
	default:
		writeJSON(w, 400, map[string]string{"error": "unsupported_grant_type"})
	}
}

func (f *Server) authorized(r *http.Request) *account {
	t, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil
	}
	email, ok := f.access[t]
	if !ok {
		return nil
	}
	return f.accounts[email]
}

func (f *Server) gmail(w http.ResponseWriter, r *http.Request, key string, a *account) {
	switch key {
	case "profile":
		writeJSON(w, 200, map[string]string{"emailAddress": a.email, "historyId": strconv.FormatUint(a.history, 10)})
	case "list":
		q := r.URL.Query().Get("q")
		f.Queries = append(f.Queries, q)
		m := reFrom.FindStringSubmatch(q)
		if m == nil {
			writeJSON(w, 400, map[string]string{"error": "Pulse debe filtrar por remitente"})
			return
		}
		allowed := map[string]bool{}
		for _, s := range strings.Split(m[1], " OR ") {
			allowed[strings.TrimSpace(s)] = true
		}
		var ids []map[string]string
		for _, msg := range a.messages {
			if allowed[msg.From] {
				ids = append(ids, map[string]string{"id": msg.ID})
			}
		}
		// Paginación de 2 en 2 para probar el recorrido de páginas.
		start, _ := strconv.Atoi(r.URL.Query().Get("pageToken"))
		end := min(start+2, len(ids))
		out := map[string]any{"messages": ids[start:end], "resultSizeEstimate": len(ids)}
		if end < len(ids) {
			out["nextPageToken"] = strconv.Itoa(end)
		}
		writeJSON(w, 200, out)
	case "raw":
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		for _, msg := range a.messages {
			if msg.ID == id {
				f.RawFetched = append(f.RawFetched, id)
				writeJSON(w, 200, map[string]string{"id": id, "raw": base64.URLEncoding.EncodeToString(msg.Raw)})
				return
			}
		}
		writeJSON(w, 404, map[string]any{"error": map[string]any{"code": 404}})
	case "watch":
		writeJSON(w, 200, map[string]string{"historyId": strconv.FormatUint(a.history, 10),
			"expiration": strconv.FormatInt(time.Now().Add(7*24*time.Hour).UnixMilli(), 10)})
	case "stop":
		f.Stopped++
		w.WriteHeader(204)
	}
}

// RotateKey cambia la clave con la que "Google" firma (mismo kid).
func (f *Server) RotateKey() {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	f.mu.Lock()
	f.Key = k
	f.mu.Unlock()
}

// SignJWT firma un token como el que Pub/Sub adjunta a sus avisos.
func (f *Server) SignJWT(claims map[string]any) string {
	f.mu.Lock()
	k := f.Key
	f.mu.Unlock()
	return SignJWT(k, Kid, claims)
}

func SignJWT(key *rsa.PrivateKey, kid string, claims map[string]any) string {
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	p, _ := json.Marshal(claims)
	in := b64(h) + "." + b64(p)
	sum := sha256.Sum256([]byte(in))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	return in + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func randID() string {
	b := make([]byte, 9)
	rand.Read(b)
	return b64(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Package gmail conecta Pulse con el Gmail de cada usuario (fase 7):
//
//	OAuth (PKCE + state firmado) ─► refresh token cifrado con la DEK del usuario
//	polling cada N min  ┐
//	aviso de Pub/Sub ───┴─► messages.list SOLO con remitentes bancarios ─► raw ─► mailparse ─► importer
//
// Privacidad: Pulse nunca lista ni descarga correos que no sean de los bancos soportados
// (la consulta va filtrada por remitente en Google) y no guarda ningún correo: se procesa en
// memoria y se descarta. Solo queda el HMAC del id del mensaje para no repetirlo.
package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Scope mínimo que permite leer el contenido de un correo.
const Scope = "https://www.googleapis.com/auth/gmail.readonly"

// Endpoints de Google. En los tests y en E2E apuntan a un servidor falso.
type Endpoints struct {
	Auth, Token, Revoke, Gmail, Certs, Gemini string
}

func GoogleEndpoints() Endpoints {
	return Endpoints{
		Auth:   "https://accounts.google.com/o/oauth2/v2/auth",
		Token:  "https://oauth2.googleapis.com/token",
		Revoke: "https://oauth2.googleapis.com/revoke",
		Gmail:  "https://gmail.googleapis.com/gmail/v1/users/me",
		Certs:  "https://www.googleapis.com/oauth2/v3/certs",
		Gemini: "https://generativelanguage.googleapis.com/v1beta",
	}
}

// FakeEndpoints apunta todo a un único servidor falso (solo desarrollo/E2E).
func FakeEndpoints(base string) Endpoints {
	base = strings.TrimRight(base, "/")
	return Endpoints{Auth: base + "/o/oauth2/v2/auth", Token: base + "/token", Revoke: base + "/revoke",
		Gmail: base + "/gmail/v1/users/me", Certs: base + "/certs", Gemini: base + "/gemini"}
}

// Client habla con OAuth y la API de Gmail. Sin estado: los tokens los maneja Syncer.
type Client struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	EP           Endpoints
	HTTP         *http.Client
}

// NewHTTPClient: tiempos acotados y sin seguir redirecciones (una API no debería redirigir).
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// APIError es una respuesta de error de Google. Code es corto y seguro para guardar/mostrar.
type APIError struct {
	Status     int
	Code       string
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return fmt.Sprintf("google: %d %s", e.Status, e.Code) }

// ErrRevoked: el refresh token ya no vale (la persona quitó el acceso o lo cambió en Google).
var ErrRevoked = errors.New("gmail: acceso revocado")

// Temporary indica si conviene reintentar más tarde.
func Temporary(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status == 429 || ae.Status >= 500 || ae.Status == 408
	}
	var ne net.Error
	var ue *url.Error
	return errors.As(err, &ne) || errors.As(err, &ue) || errors.Is(err, context.DeadlineExceeded) // red, timeout
}

func (c *Client) AuthURL(state, challenge, loginHint string) string {
	q := url.Values{
		"client_id":              {c.ClientID},
		"redirect_uri":           {c.RedirectURL},
		"response_type":          {"code"},
		"scope":                  {Scope},
		"access_type":            {"offline"},
		"prompt":                 {"consent"}, // garantiza un refresh token nuevo al reconectar
		"include_granted_scopes": {"false"},
		"state":                  {state},
		"code_challenge":         {challenge},
		"code_challenge_method":  {"S256"},
	}
	if loginHint != "" {
		q.Set("login_hint", loginHint)
	}
	return c.EP.Auth + "?" + q.Encode()
}

type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

func (c *Client) token(ctx context.Context, form url.Values) (*Token, error) {
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, "POST", c.EP.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != 200 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(body, &e)
		if e.Error == "invalid_grant" || e.Error == "unauthorized_client" {
			return nil, ErrRevoked
		}
		return nil, apiError(res, "token_"+safeCode(e.Error))
	}
	var t Token
	if err := json.Unmarshal(body, &t); err != nil || t.AccessToken == "" {
		return nil, &APIError{Status: 502, Code: "token_invalido"}
	}
	return &t, nil
}

// Exchange canjea el código de autorización (con el verificador PKCE).
func (c *Client) Exchange(ctx context.Context, code, verifier string) (*Token, error) {
	t, err := c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "redirect_uri": {c.RedirectURL}})
	if errors.Is(err, ErrRevoked) {
		return nil, &APIError{Status: 400, Code: "codigo_invalido"}
	}
	return t, err
}

func (c *Client) Refresh(ctx context.Context, refresh string) (*Token, error) {
	return c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
}

// Revoke invalida el token en Google (al desconectar). Un token ya inválido no es error.
func (c *Client) Revoke(ctx context.Context, tok string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", c.EP.Revoke, strings.NewReader(url.Values{"token": {tok}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 400 {
		return apiError(res, "revoke")
	}
	return nil
}

func (c *Client) call(ctx context.Context, method, access, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.EP.Gmail+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		return apiError(res, "gmail")
	}
	if out == nil {
		return nil
	}
	// 3 MB: un mensaje raw de hasta 2 MB en base64.
	return json.NewDecoder(io.LimitReader(res.Body, 3<<20)).Decode(out)
}

type Profile struct {
	EmailAddress string `json:"emailAddress"`
	HistoryID    string `json:"historyId"`
}

func (c *Client) Profile(ctx context.Context, access string) (*Profile, error) {
	var p Profile
	if err := c.call(ctx, "GET", access, "/profile", nil, &p); err != nil {
		return nil, err
	}
	if !strings.Contains(p.EmailAddress, "@") {
		return nil, &APIError{Status: 502, Code: "perfil_invalido"}
	}
	return &p, nil
}

// List devuelve ids de mensajes que cumplen la consulta (Gmail ordena del más nuevo al más viejo).
func (c *Client) List(ctx context.Context, access, query, pageToken string) (ids []string, next string, err error) {
	q := url.Values{"q": {query}, "maxResults": {"100"}, "includeSpamTrash": {"false"}}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	var out struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := c.call(ctx, "GET", access, "/messages?"+q.Encode(), nil, &out); err != nil {
		return nil, "", err
	}
	for _, m := range out.Messages {
		if validID(m.ID) {
			ids = append(ids, m.ID)
		}
	}
	return ids, out.NextPageToken, nil
}

// Raw descarga el mensaje completo (RFC 822) para pasarlo al mismo lector que los .eml.
func (c *Client) Raw(ctx context.Context, access, id string) ([]byte, error) {
	if !validID(id) {
		return nil, &APIError{Status: 400, Code: "id_invalido"}
	}
	var out struct {
		Raw string `json:"raw"`
	}
	if err := c.call(ctx, "GET", access, "/messages/"+id+"?format=raw", nil, &out); err != nil {
		return nil, err
	}
	b, err := base64.URLEncoding.DecodeString(out.Raw)
	if err != nil {
		b, err = base64.RawURLEncoding.DecodeString(out.Raw)
	}
	if err != nil {
		return nil, &APIError{Status: 502, Code: "raw_invalido"}
	}
	return b, nil
}

// Watch pide a Gmail que avise a Pub/Sub de cambios en el buzón (caduca a los 7 días).
func (c *Client) Watch(ctx context.Context, access, topic string) (historyID uint64, expires time.Time, err error) {
	var out struct {
		HistoryID  string `json:"historyId"`
		Expiration string `json:"expiration"`
	}
	// Sin filtro de etiquetas: los correos del banco pueden saltarse la bandeja por un filtro.
	// El aviso solo trae el email y un historyId; el contenido sigue filtrado por remitente.
	in := map[string]any{"topicName": topic}
	if err := c.call(ctx, "POST", access, "/watch", in, &out); err != nil {
		return 0, time.Time{}, err
	}
	h, _ := strconv.ParseUint(out.HistoryID, 10, 64)
	ms, err := strconv.ParseInt(out.Expiration, 10, 64)
	if err != nil {
		return 0, time.Time{}, &APIError{Status: 502, Code: "watch_invalido"}
	}
	return h, time.UnixMilli(ms), nil
}

func (c *Client) Stop(ctx context.Context, access string) error {
	return c.call(ctx, "POST", access, "/stop", map[string]any{}, nil)
}

func apiError(res *http.Response, code string) *APIError {
	e := &APIError{Status: res.StatusCode, Code: code}
	if v := res.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			e.RetryAfter = time.Duration(min(n, 3600)) * time.Second
		}
	}
	return e
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func safeCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 40 {
			break
		}
	}
	return b.String()
}

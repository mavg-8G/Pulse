package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Destination recibe el respaldo YA cifrado. Ninguna implementación ve datos en claro.
type Destination interface {
	// Put sube body (size bytes, SHA-256 hexSum) con el nombre dado. No debe sobrescribir respaldos ajenos:
	// los nombres llevan fecha y hora.
	Put(ctx context.Context, name string, body io.ReadSeeker, size int64, hexSum string) error
	// Describe devuelve un texto sin credenciales para mostrar en Ajustes.
	Describe() string
}

// PermanentError marca un fallo que no se arregla reintentando (credenciales, permisos, bucket inexistente).
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// statusError traduce una respuesta HTTP fallida: 4xx (salvo 408 y 429) no se reintenta.
func statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.Join(strings.Fields(string(body)), " ")
	if len(msg) > 200 {
		msg = msg[:200]
	}
	err := fmt.Errorf("el destino respondió %d %s", resp.StatusCode, msg)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 && resp.StatusCode != 429 {
		return &PermanentError{err}
	}
	return err
}

func put(ctx context.Context, c *http.Client, req *http.Request) error {
	resp, err := c.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return statusError(resp)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}

// ---------------- S3 compatible (AWS, Cloudflare R2, Backblaze B2, MinIO, Wasabi…) ----------------

// S3 sube con PUT firmado con AWS Signature V4. Basta una credencial con permiso PutObject sobre el
// prefijo: el servidor no necesita leer ni borrar (la retención la hace una regla del bucket).
type S3 struct {
	Endpoint  *url.URL // https://s3.us-east-1.amazonaws.com, https://<cuenta>.r2.cloudflarestorage.com…
	Region    string
	Bucket    string
	Prefix    string // p. ej. "pulse/"
	AccessKey string
	SecretKey string
	PathStyle bool // https://endpoint/bucket/clave (MinIO, R2) en vez de https://bucket.endpoint/clave
	Client    *http.Client
	Now       func() time.Time
}

func (s *S3) Describe() string {
	return fmt.Sprintf("S3 · %s/%s en %s", s.Bucket, strings.TrimSuffix(s.Prefix, "/"), s.Endpoint.Host)
}

// uriEncode codifica como pide SigV4: todo menos A-Z a-z 0-9 - _ . ~ (y "/" si keepSlash).
func uriEncode(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' || c == '/' && keepSlash {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// request arma el PUT firmado. Separado de Put para probarlo con vectores fijos.
func (s *S3) request(name string, body io.Reader, size int64, hexSum string, now time.Time) (*http.Request, error) {
	key := s.Prefix + name
	u := *s.Endpoint
	if s.PathStyle {
		u.Path = "/" + s.Bucket + "/" + key
	} else {
		u.Host = s.Bucket + "." + u.Host
		u.Path = "/" + key
	}
	canonicalURI := uriEncode(u.Path, true)
	u.RawPath = canonicalURI
	req, err := http.NewRequest(http.MethodPut, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size
	amzDate := now.UTC().Format("20060102T150405Z")
	day := amzDate[:8]
	headers := map[string]string{
		"content-type":         "application/octet-stream",
		"host":                 u.Host,
		"x-amz-content-sha256": hexSum,
		"x-amz-date":           amzDate,
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{http.MethodPut, canonicalURI, "", canonHeaders.String(), signed, hexSum}, "\n")
	scope := day + "/" + s.Region + "/s3/aws4_request"
	sum := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
	k := hmacSHA256([]byte("AWS4"+s.SecretKey), day)
	k = hmacSHA256(k, s.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	for _, n := range names {
		if n != "host" {
			req.Header.Set(n, headers[n])
		}
	}
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
	return req, nil
}

func (s *S3) Put(ctx context.Context, name string, body io.ReadSeeker, size int64, hexSum string) error {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	req, err := s.request(name, body, size, hexSum, now())
	if err != nil {
		return &PermanentError{err}
	}
	return put(ctx, s.client(), req)
}

func (s *S3) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return defaultClient
}

// ---------------- WebDAV (Nextcloud, ownCloud…) ----------------

// WebDAV sube con PUT y autenticación básica a una carpeta (p. ej. una contraseña de aplicación de Nextcloud).
type WebDAV struct {
	URL      *url.URL // carpeta: https://nube.ejemplo/remote.php/dav/files/<usuario>/pulse-backups
	User     string
	Password string
	Client   *http.Client
}

func (w *WebDAV) Describe() string {
	return fmt.Sprintf("WebDAV · %s%s", w.URL.Host, w.URL.Path)
}

func (w *WebDAV) Put(ctx context.Context, name string, body io.ReadSeeker, size int64, hexSum string) error {
	u := *w.URL
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + name
	req, err := http.NewRequest(http.MethodPut, u.String(), body)
	if err != nil {
		return &PermanentError{err}
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("OC-Checksum", "SHA256:"+hexSum) // Nextcloud lo guarda y lo verifica
	req.SetBasicAuth(w.User, w.Password)
	c := w.Client
	if c == nil {
		c = defaultClient
	}
	return put(ctx, c, req)
}

// defaultClient no sigue redirecciones: una credencial no debe viajar a otro sitio.
var defaultClient = &http.Client{
	Timeout:       10 * time.Minute,
	CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("el destino redirige; usa la URL final") },
}

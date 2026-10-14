package gmail

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PubSubVerifier comprueba el JWT (OIDC) que Google adjunta a cada aviso push de Pub/Sub.
// Sin un JWT válido —firma de Google, audiencia de Pulse, vigente y emitido para la cuenta
// de servicio configurada— el aviso se rechaza sin procesar nada.
type PubSubVerifier struct {
	CertsURL       string
	Audience       string
	ServiceAccount string
	HTTP           *http.Client
	Now            func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expires   time.Time
	lastFetch time.Time
}

var ErrJWT = errors.New("jwt inválido")

type jwtClaims struct {
	Iss           string `json:"iss"`
	Aud           string `json:"aud"`
	Exp           int64  `json:"exp"`
	Iat           int64  `json:"iat"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

func (v *PubSubVerifier) Verify(ctx context.Context, authz string) error {
	tok, ok := strings.CutPrefix(authz, "Bearer ")
	if !ok || len(tok) > 4096 {
		return ErrJWT
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return ErrJWT
	}
	hb, err1 := b64.DecodeString(parts[0])
	pb, err2 := b64.DecodeString(parts[1])
	sig, err3 := b64.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return ErrJWT
	}
	var h struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(hb, &h) != nil || h.Alg != "RS256" || h.Kid == "" { // nunca "none" ni HS256
		return ErrJWT
	}
	key, err := v.key(ctx, h.Kid)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig) != nil {
		return ErrJWT
	}
	var c jwtClaims
	if json.Unmarshal(pb, &c) != nil {
		return ErrJWT
	}
	now := v.Now().Unix()
	const skew = 60
	switch {
	case c.Iss != "https://accounts.google.com" && c.Iss != "accounts.google.com":
	case c.Aud != v.Audience:
	case c.Exp < now-skew || c.Iat > now+skew || c.Exp-c.Iat > 3600+skew:
	case !c.EmailVerified || !strings.EqualFold(c.Email, v.ServiceAccount):
	default:
		return nil
	}
	return ErrJWT
}

// key busca la clave pública por kid; recarga las claves de Google si caducaron o si aparece
// un kid nuevo (rotación), como mucho una vez por minuto para no amplificar ataques.
func (v *PubSubVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.Now()
	if k, ok := v.keys[kid]; ok && now.Before(v.expires) {
		return k, nil
	}
	if now.Sub(v.lastFetch) < time.Minute && v.keys != nil {
		if k, ok := v.keys[kid]; ok {
			return k, nil
		}
		return nil, ErrJWT
	}
	v.lastFetch = now
	keys, maxAge, err := fetchJWKS(ctx, v.HTTP, v.CertsURL)
	if err != nil {
		return nil, err
	}
	v.keys, v.expires = keys, now.Add(maxAge)
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, ErrJWT
}

func fetchJWKS(ctx context.Context, c *http.Client, u string) (map[string]*rsa.PublicKey, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, 0, err
	}
	res, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, 0, apiError(res, "certs")
	}
	var set struct {
		Keys []struct {
			Kid, Kty, Alg, N, E string
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&set); err != nil {
		return nil, 0, err
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		n, err1 := b64.DecodeString(k.N)
		e, err2 := b64.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(n) < 256 || len(e) > 4 {
			continue // claves de menos de 2048 bits no se aceptan
		}
		out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	maxAge := time.Hour
	for _, d := range strings.Split(res.Header.Get("Cache-Control"), ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(d), "max-age="); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				maxAge = time.Duration(min(n, 86400)) * time.Second
			}
		}
	}
	return out, maxAge, nil
}

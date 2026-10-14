package gmail_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"pulse/internal/gmail"
	"pulse/internal/gmail/gmailtest"
)

const (
	aud = "https://pulse.test/api/gmail/webhook"
	sa  = "pubsub-push@pulse-123456.iam.gserviceaccount.com"
)

func validClaims(now time.Time) map[string]any {
	return map[string]any{"iss": "https://accounts.google.com", "aud": aud, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"email": sa, "email_verified": true, "sub": "123"}
}

func TestJWTDePubSub(t *testing.T) {
	g := gmailtest.New()
	defer g.Close()
	now := time.Now()
	v := &gmail.PubSubVerifier{CertsURL: g.Endpoints().Certs, Audience: aud, ServiceAccount: sa, HTTP: gmail.NewHTTPClient(), Now: func() time.Time { return now }}
	ctx := context.Background()
	if err := v.Verify(ctx, "Bearer "+g.SignJWT(validClaims(now))); err != nil {
		t.Fatalf("JWT válido rechazado: %v", err)
	}
	mod := func(k string, val any) string {
		c := validClaims(now)
		if val == nil {
			delete(c, k)
		} else {
			c[k] = val
		}
		return g.SignJWT(c)
	}
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	good := g.SignJWT(validClaims(now))
	parts := strings.Split(good, ".")
	cases := map[string]string{
		"audiencia de otro servicio": mod("aud", "https://otro.example/webhook"),
		"caducado":                   mod("exp", now.Add(-5*time.Minute).Unix()),
		"emitido en el futuro":       mod("iat", now.Add(10*time.Minute).Unix()),
		"vida demasiado larga":       mod("exp", now.Add(48*time.Hour).Unix()),
		"otra cuenta de servicio":    mod("email", "atacante@evil-123456.iam.gserviceaccount.com"),
		"email sin verificar":        mod("email_verified", false),
		"emisor ajeno":               mod("iss", "https://evil.example"),
		"firmado con otra clave":     gmailtest.SignJWT(otherKey, gmailtest.Kid, validClaims(now)),
		"kid desconocido":            gmailtest.SignJWT(otherKey, "otra", validClaims(now)),
		"firma alterada":             parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-4] + "AAAA",
		"payload alterado":           parts[0] + "." + strings.Split(mod("email", "x@y.iam.gserviceaccount.com"), ".")[1] + "." + parts[2],
		"alg none":                   "eyJhbGciOiJub25lIiwia2lkIjoiY2xhdmUtMSJ9." + parts[1] + ".",
		"alg HS256":                  "eyJhbGciOiJIUzI1NiIsImtpZCI6ImNsYXZlLTEifQ." + parts[1] + "." + parts[2],
		"basura":                     "abc",
	}
	for name, tok := range cases {
		if err := v.Verify(ctx, "Bearer "+tok); !errors.Is(err, gmail.ErrJWT) {
			t.Errorf("%s: debía rechazarse, fue %v", name, err)
		}
	}
	if err := v.Verify(ctx, good); err == nil {
		t.Error("sin prefijo Bearer debe rechazarse")
	}
	// Las claves se cachean: decenas de avisos no generan decenas de descargas.
	if g.Count("certs") > 2 {
		t.Fatalf("certificados descargados %d veces", g.Count("certs"))
	}
}

func TestJWTRotacionDeClaves(t *testing.T) {
	g := gmailtest.New()
	defer g.Close()
	now := time.Now()
	clock := func() time.Time { return now }
	v := &gmail.PubSubVerifier{CertsURL: g.Endpoints().Certs, Audience: aud, ServiceAccount: sa, HTTP: gmail.NewHTTPClient(), Now: clock}
	ctx := context.Background()
	if err := v.Verify(ctx, "Bearer "+g.SignJWT(validClaims(now))); err != nil {
		t.Fatal(err)
	}
	// Google rota la clave (mismo kid): la caché aún tiene la anterior.
	g.RotateKey()
	now = now.Add(2 * time.Minute)
	if err := v.Verify(ctx, "Bearer "+g.SignJWT(validClaims(now))); err == nil {
		// Mismo kid, distinta clave: hasta que caduque la caché, la firma no coincide.
		t.Fatal("una firma con clave no publicada debe rechazarse")
	}
	now = now.Add(2 * time.Hour) // la caché caducó (max-age=3600): se recargan las claves
	if err := v.Verify(ctx, "Bearer "+g.SignJWT(validClaims(now))); err != nil {
		t.Fatalf("tras recargar las claves debe aceptarse: %v", err)
	}
}

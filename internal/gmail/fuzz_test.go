package gmail_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"pulse/internal/crypto"
	"pulse/internal/gmail"
)

// Entradas hostiles (JWT del webhook y state del callback) nunca deben provocar pánicos
// ni aceptarse por accidente.
func FuzzVerifyJWT(f *testing.F) {
	f.Add("Bearer eyJhbGciOiJSUzI1NiIsImtpZCI6ImsifQ.e30.c2ln")
	f.Add("Bearer a.b.c")
	f.Add("Bearer ...")
	f.Add("")
	v := &gmail.PubSubVerifier{CertsURL: "http://127.0.0.1:1/certs", Audience: "a", ServiceAccount: "s", HTTP: &http.Client{Timeout: time.Millisecond}, Now: time.Now}
	f.Fuzz(func(t *testing.T, authz string) {
		if v.Verify(context.Background(), authz) == nil {
			t.Fatalf("aceptado sin claves: %q", authz)
		}
	})
}

func FuzzCheckState(f *testing.F) {
	keys, _ := crypto.NewKeyring(bytes.Repeat([]byte{3}, 32))
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	good, _ := gmail.NewState(keys.Lookup, "u", "s", now)
	f.Add(good[:len(good)-2] + "AA") // payload válido con la firma tocada
	f.Add("x.y")
	f.Add(".")
	f.Fuzz(func(t *testing.T, state string) {
		if _, err := gmail.CheckState(keys.Lookup, state, "u", "s", now); err == nil {
			t.Fatalf("state alterado aceptado: %q", state)
		}
	})
}

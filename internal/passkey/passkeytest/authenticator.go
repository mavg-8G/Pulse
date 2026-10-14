// Package passkeytest es un autenticador WebAuthn por software para tests:
// genera credenciales P-256 y firma aserciones como lo haría un dispositivo real.
package passkeytest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"sort"

	"pulse/internal/passkey"
)

type Authenticator struct {
	RPID, Origin string
	Key          *ecdsa.PrivateKey
	CredID       []byte
	Count        uint32
	// Para pruebas negativas:
	NoUserPresence bool
	BadOrigin      string
}

func New(rpID, origin string) *Authenticator {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 32)
	rand.Read(id)
	return &Authenticator{RPID: rpID, Origin: origin, Key: k, CredID: id}
}

// ---- codificador CBOR mínimo (determinista) ----

func head(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	case n < 65536:
		b := []byte{major<<5 | 25, 0, 0}
		binary.BigEndian.PutUint16(b[1:], uint16(n))
		return b
	default:
		b := []byte{major<<5 | 26, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(b[1:], uint32(n))
		return b
	}
}

func Enc(v any) []byte {
	switch x := v.(type) {
	case int:
		if x >= 0 {
			return head(0, uint64(x))
		}
		return head(1, uint64(-1-x))
	case []byte:
		return append(head(2, uint64(len(x))), x...)
	case string:
		return append(head(3, uint64(len(x))), x...)
	case map[any]any:
		keys := make([]any, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return bytes.Compare(Enc(keys[i]), Enc(keys[j])) < 0 })
		out := head(5, uint64(len(x)))
		for _, k := range keys {
			out = append(out, Enc(k)...)
			out = append(out, Enc(x[k])...)
		}
		return out
	}
	panic("tipo no soportado")
}

func (a *Authenticator) COSE() []byte {
	x := a.Key.PublicKey.X.FillBytes(make([]byte, 32))
	y := a.Key.PublicKey.Y.FillBytes(make([]byte, 32))
	return Enc(map[any]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
}

func (a *Authenticator) clientData(typ string, challenge []byte) []byte {
	origin := a.Origin
	if a.BadOrigin != "" {
		origin = a.BadOrigin
	}
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": base64.RawURLEncoding.EncodeToString(challenge), "origin": origin, "crossOrigin": false})
	return b
}

func (a *Authenticator) authData(withCred bool) []byte {
	h := sha256.Sum256([]byte(a.RPID))
	flags := byte(0x01 | 0x04)
	if a.NoUserPresence {
		flags = 0x04
	}
	if withCred {
		flags |= 0x40
	}
	out := append([]byte(nil), h[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, a.Count)
	if withCred {
		out = append(out, make([]byte, 16)...) // AAGUID
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.CredID)))
		out = append(out, a.CredID...)
		out = append(out, a.COSE()...)
	}
	return out
}

// Register simula navigator.credentials.create() a partir de las opciones del servidor.
func (a *Authenticator) Register(challenge []byte) *passkey.RegistrationResponse {
	r := &passkey.RegistrationResponse{ID: base64.RawURLEncoding.EncodeToString(a.CredID), RawID: a.CredID, Type: "public-key"}
	r.Response.ClientDataJSON = a.clientData("webauthn.create", challenge)
	r.Response.AttestationObject = Enc(map[any]any{"fmt": "none", "attStmt": map[any]any{}, "authData": a.authData(true)})
	return r
}

// Login simula navigator.credentials.get(); incrementa el contador.
func (a *Authenticator) Login(challenge []byte) *passkey.AssertionResponse {
	a.Count++
	r := &passkey.AssertionResponse{ID: base64.RawURLEncoding.EncodeToString(a.CredID), RawID: a.CredID, Type: "public-key"}
	r.Response.ClientDataJSON = a.clientData("webauthn.get", challenge)
	r.Response.AuthenticatorData = a.authData(false)
	h := sha256.Sum256(r.Response.ClientDataJSON)
	digest := sha256.Sum256(append(append([]byte(nil), r.Response.AuthenticatorData...), h[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, a.Key, digest[:])
	r.Response.Signature = sig
	return r
}

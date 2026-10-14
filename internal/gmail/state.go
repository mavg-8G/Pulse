package gmail

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// El flujo OAuth no guarda nada en la BD antes del callback:
//   - state = payload firmado con HMAC (usuario, sesión, nonce, caducidad), atado a la sesión
//     que inició la conexión: un state robado no sirve en otra sesión ni a otro usuario.
//   - verificador PKCE = HMAC(nonce) con una clave del servidor: nunca viaja en la URL y no
//     se puede recalcular sin la KEK.

const stateTTL = 10 * time.Minute

var ErrState = errors.New("state inválido o caducado")

// MAC es el HMAC con propósito del servidor (crypto.Keyring.Lookup).
type MAC func(purpose string, data []byte) []byte

type statePayload struct {
	U string `json:"u"`
	S string `json:"s"`
	N string `json:"n"`
	E int64  `json:"e"`
}

var b64 = base64.RawURLEncoding

// NewState devuelve el state para la URL y el desafío PKCE (S256).
func NewState(mac MAC, userID, sessionID string, now time.Time) (state, challenge string) {
	nonce := make([]byte, 18)
	rand.Read(nonce)
	p, _ := json.Marshal(statePayload{U: userID, S: sessionID, N: b64.EncodeToString(nonce), E: now.Add(stateTTL).Unix()})
	enc := b64.EncodeToString(p)
	sig := b64.EncodeToString(mac("gmail-state", []byte(enc)))
	sum := sha256.Sum256([]byte(verifier(mac, b64.EncodeToString(nonce))))
	return enc + "." + sig, b64.EncodeToString(sum[:])
}

// CheckState valida el state del callback y devuelve el verificador PKCE.
func CheckState(mac MAC, state, userID, sessionID string, now time.Time) (string, error) {
	enc, sig, ok := strings.Cut(state, ".")
	if !ok || len(state) > 512 {
		return "", ErrState
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac("gmail-state", []byte(enc))) {
		return "", ErrState
	}
	raw, err := b64.DecodeString(enc)
	if err != nil {
		return "", ErrState
	}
	var p statePayload
	if json.Unmarshal(raw, &p) != nil || p.U != userID || p.S != sessionID || now.Unix() > p.E || p.N == "" {
		return "", ErrState
	}
	return verifier(mac, p.N), nil
}

func verifier(mac MAC, nonce string) string {
	return b64.EncodeToString(mac("gmail-pkce", []byte(nonce))) // 43 caracteres: lo que exige RFC 7636
}

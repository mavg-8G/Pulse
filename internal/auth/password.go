package auth

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"pulse/internal/crypto"
)

// DefaultIterations: recomendación OWASP para PBKDF2-HMAC-SHA256.
const DefaultIterations = 600_000

// Hasher aplica pimienta (HMAC con clave del servidor) + PBKDF2-SHA256 con sal aleatoria.
// El formato lleva el algoritmo y las iteraciones, así se puede subir el costo o
// migrar a argon2id rehaciendo el hash en el siguiente login.
type Hasher struct {
	Keys       *crypto.Keyring
	Iterations int
}

func (h *Hasher) iter() int {
	if h.Iterations > 0 {
		return h.Iterations
	}
	return DefaultIterations
}

func (h *Hasher) Hash(password string) (string, error) {
	salt := crypto.RandomBytes(16)
	key, err := pbkdf2.Key(sha256.New, string(h.Keys.Pepper(password)), salt, h.iter(), 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", h.iter(),
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify devuelve si coincide y si conviene rehacer el hash (iteraciones viejas).
func (h *Hasher) Verify(encoded, password string) (ok, rehash bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, false
	}
	it, err := strconv.Atoi(parts[1])
	if err != nil || it < 1000 || it > 10_000_000 {
		return false, false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) != 32 {
		return false, false
	}
	got, err := pbkdf2.Key(sha256.New, string(h.Keys.Pepper(password)), salt, it, 32)
	if err != nil {
		return false, false
	}
	ok = subtle.ConstantTimeCompare(got, want) == 1
	return ok, ok && it < h.iter()
}

// DummyVerify gasta el mismo tiempo que una verificación real; se usa cuando el
// email no existe para no revelar qué cuentas hay (ataque de tiempo).
func (h *Hasher) DummyVerify(password string) {
	pbkdf2.Key(sha256.New, string(h.Keys.Pepper(password)), []byte("pulse-dummy-salt"), h.iter(), 32)
}

var commonPasswords = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`123456789012 1234567890123 12345678901234 123456789012345
		password1234 password12345 password123456 passwordpassword contraseña123 contraseña1234
		contrasena123 contrasena1234 qwertyuiopas qwertyuiop123 qwerty123456 qwerty1234567
		asdfghjklñ12 asdfghjkl123 iloveyou1234 teamo1234567 teamomucho12 abc123456789
		abcdefghijkl abcdefgh1234 000000000000 111111111111 123123123123 aaaaaaaaaaaa
		administrator admin1234567 adminadmin12 welcome12345 bienvenido12 bienvenido123
		ecuador12345 ecuador2024! ecuador2025! ecuador2026! guayaquil123 barcelonasc1
		emelec123456 pichincha123 produbanco12 pulsepulse12 pulse1234567 letmein12345
		football1234 futbol123456 superman1234 batman123456 dragon123456 monkey123456
		sunshine1234 princess1234 starwars1234 trustno1trust 1q2w3e4r5t6y 1qaz2wsx3edc
		zaq12wsxcde3 p@ssw0rd1234 passw0rd1234 changeme1234 cambiame1234`) {
		commonPasswords[strings.ToLower(p)] = true
	}
}

var ErrWeakPassword = errors.New("la contraseña debe tener entre 12 y 128 caracteres y no ser una contraseña común")

// CheckPolicy valida longitud, lista de contraseñas comunes y que no contenga el email.
func CheckPolicy(password, email string) error {
	n := utf8.RuneCountInString(password)
	if n < 12 || n > 128 {
		return ErrWeakPassword
	}
	lp := strings.ToLower(password)
	if commonPasswords[lp] {
		return ErrWeakPassword
	}
	if local, _, ok := strings.Cut(strings.ToLower(email), "@"); ok && len(local) >= 4 && strings.Contains(lp, local) {
		return errors.New("la contraseña no debe contener tu email")
	}
	distinct := map[rune]bool{}
	for _, r := range password {
		distinct[r] = true
	}
	if len(distinct) < 5 {
		return ErrWeakPassword
	}
	return nil
}

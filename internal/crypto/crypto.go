// Package crypto implementa el cifrado por sobre de Pulse:
//
//	KEK (clave maestra, Docker secret) ──HKDF──► clave de envoltura ──AES-GCM──► DEK por usuario
//	DEK por usuario ──AES-GCM──► campos sensibles (comercio, notas, secreto TOTP…)
//
// Cada texto cifrado lleva como dato asociado el id del usuario y el nombre del campo,
// así que no puede copiarse a otra fila, a otro campo ni a otro usuario sin que falle.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	KeySize = 32
	version = byte(1)
)

var ErrDecrypt = errors.New("no se pudo descifrar (dato alterado o clave incorrecta)")

// Keyring guarda las subclaves derivadas de la KEK. La KEK cruda no se conserva.
type Keyring struct {
	wrap   cipher.AEAD
	lookup []byte // HMAC para buscar tokens/códigos sin guardarlos en claro
	pepper []byte // HMAC aplicado a contraseñas antes del hash lento
}

func derive(kek []byte, info string) []byte {
	k, err := hkdf.Key(sha256.New, kek, nil, info, KeySize)
	if err != nil {
		panic(err) // solo falla con longitudes imposibles
	}
	return k
}

func newGCM(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return g
}

// NewKeyring crea el llavero a partir de una KEK de 32 bytes.
func NewKeyring(kek []byte) (*Keyring, error) {
	if len(kek) != KeySize {
		return nil, fmt.Errorf("la KEK debe tener %d bytes, tiene %d", KeySize, len(kek))
	}
	allZero := true
	for _, b := range kek {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil, errors.New("la KEK no puede ser todo ceros")
	}
	return &Keyring{
		wrap:   newGCM(derive(kek, "pulse/v1/dek-wrap")),
		lookup: derive(kek, "pulse/v1/lookup"),
		pepper: derive(kek, "pulse/v1/password-pepper"),
	}, nil
}

// LoadKEKFile lee la KEK en base64 desde un archivo (Docker secret).
func LoadKEKFile(path string) (*Keyring, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("leyendo la KEK: %w", err)
	}
	kek, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, errors.New("la KEK debe estar en base64")
	}
	return NewKeyring(kek)
}

// GenerateKEK devuelve una KEK nueva en base64.
func GenerateKEK() string {
	return base64.StdEncoding.EncodeToString(RandomBytes(KeySize))
}

func RandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func seal(g cipher.AEAD, plaintext, aad []byte) []byte {
	nonce := RandomBytes(g.NonceSize())
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+g.Overhead())
	out = append(out, version)
	out = append(out, nonce...)
	return g.Seal(out, nonce, plaintext, aad)
}

func open(g cipher.AEAD, blob, aad []byte) ([]byte, error) {
	ns := g.NonceSize()
	if len(blob) < 1+ns+g.Overhead() || blob[0] != version {
		return nil, ErrDecrypt
	}
	pt, err := g.Open(nil, blob[1:1+ns], blob[1+ns:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// NewDEK genera una DEK y la devuelve envuelta con la KEK, lista para guardar.
func (k *Keyring) NewDEK(userID string) (wrapped []byte) {
	return seal(k.wrap, RandomBytes(KeySize), []byte("dek|"+userID))
}

// Sealer descifra la DEK de un usuario y devuelve un cifrador para sus campos.
func (k *Keyring) Sealer(userID string, wrapped []byte) (*Sealer, error) {
	dek, err := open(k.wrap, wrapped, []byte("dek|"+userID))
	if err != nil {
		return nil, err
	}
	return &Sealer{g: newGCM(dek), userID: userID}, nil
}

// Lookup calcula un HMAC con propósito para guardar y buscar secretos (tokens, códigos).
func (k *Keyring) Lookup(purpose string, data []byte) []byte {
	m := hmac.New(sha256.New, k.lookup)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write(data)
	return m.Sum(nil)
}

// Pepper mezcla la contraseña con una clave del servidor: un volcado de la BD
// sin la KEK no permite atacar las contraseñas offline.
func (k *Keyring) Pepper(password string) []byte {
	m := hmac.New(sha256.New, k.pepper)
	m.Write([]byte(password))
	return m.Sum(nil)
}

// Sealer cifra campos de un usuario concreto.
type Sealer struct {
	g      cipher.AEAD
	userID string
}

func (s *Sealer) aad(field string) []byte { return []byte(s.userID + "|" + field) }

// Seal cifra un texto. Un texto vacío se guarda como NULL (nil).
func (s *Sealer) Seal(field, plaintext string) []byte {
	if plaintext == "" {
		return nil
	}
	return seal(s.g, []byte(plaintext), s.aad(field))
}

// Open descifra un campo. nil → "".
func (s *Sealer) Open(field string, blob []byte) (string, error) {
	if len(blob) == 0 {
		return "", nil
	}
	pt, err := open(s.g, blob, s.aad(field))
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func (s *Sealer) SealBytes(field string, b []byte) []byte { return seal(s.g, b, s.aad(field)) }

func (s *Sealer) OpenBytes(field string, blob []byte) ([]byte, error) {
	return open(s.g, blob, s.aad(field))
}

// Equal compara en tiempo constante.
func Equal(a, b []byte) bool { return hmac.Equal(a, b) }

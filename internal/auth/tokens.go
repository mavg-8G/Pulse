// Package auth reúne las primitivas de autenticación: contraseñas, tokens, TOTP,
// códigos de recuperación, invitaciones y limitación de intentos.
package auth

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"time"
)

// Token devuelve un token aleatorio de 256 bits en base64url.
func Token() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Alfabeto sin caracteres ambiguos (sin 0/O, 1/I/L, U).
const alphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

func randomGroups(groups, size int) string {
	var sb strings.Builder
	buf := make([]byte, 1)
	for g := 0; g < groups; g++ {
		if g > 0 {
			sb.WriteByte('-')
		}
		for i := 0; i < size; {
			rand.Read(buf)
			// rechazo para evitar sesgo de módulo: 30*8 = 240
			if buf[0] >= 240 {
				continue
			}
			sb.WriteByte(alphabet[int(buf[0])%len(alphabet)])
			i++
		}
	}
	return sb.String()
}

// InviteCode: 20 símbolos de 30 posibles ≈ 98 bits de entropía.
func InviteCode() string { return "PLS-" + randomGroups(5, 4) }

// RecoveryCode: 10 símbolos ≈ 49 bits; se guardan con HMAC de la KEK y son de un solo uso.
func RecoveryCode() string { return strings.ToLower(randomGroups(2, 5)) }

// NormalizeCode quita espacios/guiones y pasa a mayúsculas para comparar.
func NormalizeCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "PLS")
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, s)
}

// NewID genera un UUIDv7 (ordenable por tiempo, no adivinable).
func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	ms := uint64(time.Now().UnixMilli())
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], ms)
	copy(b[0:6], t[2:8])
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

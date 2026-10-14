package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"pulse/internal/crypto"
)

// TOTP según RFC 6238: SHA-1, 6 dígitos, pasos de 30 s (lo que usan todas las apps).
const (
	totpPeriod = 30
	totpDigits = 6
)

func NewTOTPSecret() []byte { return crypto.RandomBytes(20) }

func TOTPSecretString(secret []byte) string { return b32.EncodeToString(secret) }

func TOTPURI(issuer, account string, secret []byte) string {
	v := url.Values{}
	v.Set("secret", TOTPSecretString(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

func hotp(secret []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, code%1_000_000)
}

func TOTPStep(t time.Time) int64 { return t.Unix() / totpPeriod }

func TOTPCode(secret []byte, t time.Time) string { return hotp(secret, uint64(TOTPStep(t))) }

// ValidateTOTP acepta el paso actual ±1 (deriva de reloj) y solo pasos mayores a
// lastStep, para que un código interceptado no pueda reutilizarse.
func ValidateTOTP(secret []byte, code string, now time.Time, lastStep int64) (step int64, ok bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	cur := TOTPStep(now)
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		if s <= lastStep {
			continue
		}
		if hmac.Equal([]byte(hotp(secret, uint64(s))), []byte(code)) {
			return s, true
		}
	}
	return 0, false
}

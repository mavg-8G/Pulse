package auth

import (
	"bytes"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
	"time"

	"pulse/internal/clock"
	"pulse/internal/crypto"
)

func hasher(t *testing.T) *Hasher {
	k, _ := crypto.NewKeyring(bytes.Repeat([]byte{3}, 32))
	return &Hasher{Keys: k, Iterations: 1000}
}

func TestPasswordHash(t *testing.T) {
	h := hasher(t)
	enc, err := h.Hash("una clave muy larga")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "pbkdf2-sha256$1000$") {
		t.Fatalf("formato inesperado: %s", enc)
	}
	if ok, _ := h.Verify(enc, "una clave muy larga"); !ok {
		t.Fatal("debería verificar")
	}
	if ok, _ := h.Verify(enc, "una clave muy largA"); ok {
		t.Fatal("no debería verificar")
	}
	enc2, _ := h.Hash("una clave muy larga")
	if enc == enc2 {
		t.Fatal("la sal debe ser aleatoria")
	}
	// Sin la pimienta (otra KEK) el hash no sirve
	k2, _ := crypto.NewKeyring(bytes.Repeat([]byte{4}, 32))
	if ok, _ := (&Hasher{Keys: k2, Iterations: 1000}).Verify(enc, "una clave muy larga"); ok {
		t.Fatal("con otra pimienta no debe verificar")
	}
	// Rehash si sube el costo
	h2 := &Hasher{Keys: h.Keys, Iterations: 2000}
	if ok, rehash := h2.Verify(enc, "una clave muy larga"); !ok || !rehash {
		t.Fatal("debe pedir rehash con más iteraciones")
	}
	for _, bad := range []string{"", "x$y", "pbkdf2-sha256$abc$a$b", "pbkdf2-sha256$5$AAAA$AAAA", "md5$1000$a$b"} {
		if ok, _ := h.Verify(bad, "x"); ok {
			t.Fatalf("hash malformado %q verificó", bad)
		}
	}
}

func TestPoliticaDeContrasenas(t *testing.T) {
	cases := map[string]bool{
		"corta":                  false,
		"password1234":           false,
		"Password1234":           false, // común, sin importar mayúsculas
		"aaaaaaaaaaaaaaa":        false, // poca variedad
		"matias.vasconez2026":    false, // contiene el email
		"caballo-bateria-grapa":  true,
		"Pulse es mi app 2026!":  true,
		strings.Repeat("ab", 70): false, // > 128
	}
	for pw, want := range cases {
		err := CheckPolicy(pw, "matias.vasconez@gmail.com")
		if (err == nil) != want {
			t.Errorf("%q: válido=%v, esperaba %v (%v)", pw, err == nil, want, err)
		}
	}
}

// Vectores del RFC 6238 (SHA-1), truncados a 6 dígitos.
func TestTOTPVectoresRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	vec := map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"}
	for ts, want := range vec {
		if got := TOTPCode(secret, time.Unix(ts, 0)); got != want {
			t.Errorf("t=%d: %s, esperaba %s", ts, got, want)
		}
	}
}

func TestTOTPVentanaYReplay(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	code := TOTPCode(secret, now)
	step, ok := ValidateTOTP(secret, code, now, 0)
	if !ok {
		t.Fatal("código actual debe valer")
	}
	if _, ok := ValidateTOTP(secret, code, now, step); ok {
		t.Fatal("el mismo código no debe poder reutilizarse")
	}
	if _, ok := ValidateTOTP(secret, TOTPCode(secret, now.Add(-30*time.Second)), now, 0); !ok {
		t.Fatal("paso anterior debe tolerarse (deriva de reloj)")
	}
	if _, ok := ValidateTOTP(secret, TOTPCode(secret, now.Add(-90*time.Second)), now, 0); ok {
		t.Fatal("código de hace 90 s no debe valer")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := ValidateTOTP(secret, bad, now, 0); ok {
			t.Fatalf("%q no debe valer", bad)
		}
	}
	if !strings.HasPrefix(TOTPURI("Pulse", "a@b.com", secret), "otpauth://totp/Pulse:a@b.com?") {
		t.Fatal("URI otpauth inesperada")
	}
}

func TestCodigosYIDs(t *testing.T) {
	re := regexp.MustCompile(`^PLS-([A-HJKMNP-TV-Z2-9]{4}-){4}[A-HJKMNP-TV-Z2-9]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		c := InviteCode()
		if !re.MatchString(c) {
			t.Fatalf("invitación con formato inválido: %s", c)
		}
		if seen[c] {
			t.Fatal("colisión de invitación")
		}
		seen[c] = true
	}
	if NormalizeCode(" pls-abcd-efgh ") != "ABCDEFGH" {
		t.Fatal("normalización incorrecta")
	}
	id := NewID()
	if len(id) != 36 || id[14] != '7' {
		t.Fatalf("UUIDv7 inválido: %s", id)
	}
	raw, _ := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	if raw[8]&0xc0 != 0x80 {
		t.Fatal("variante incorrecta")
	}
	if len(Token()) != 43 {
		t.Fatal("token debe ser de 256 bits")
	}
	if len(RecoveryCode()) != 11 {
		t.Fatal("código de recuperación con largo inesperado")
	}
}

func TestLimiter(t *testing.T) {
	c := clock.NewFake(time.Unix(0, 0))
	l := NewLimiter(c, 3, 1, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("ip") {
			t.Fatalf("intento %d debería pasar", i)
		}
	}
	if l.Allow("ip") {
		t.Fatal("el 4.º intento debe bloquearse")
	}
	if !l.Allow("otra-ip") {
		t.Fatal("las claves son independientes")
	}
	c.Advance(61 * time.Second)
	if !l.Allow("ip") {
		t.Fatal("debe recuperar una ficha por minuto")
	}
	if l.Allow("ip") {
		t.Fatal("solo una ficha recuperada")
	}
}

func TestBloqueoProgresivo(t *testing.T) {
	now := time.Unix(0, 0)
	if _, locked := LockoutUntil(now, 4); locked {
		t.Fatal("4 fallos no bloquean")
	}
	want := map[int]time.Duration{5: time.Minute, 6: 2 * time.Minute, 8: 8 * time.Minute, 20: time.Hour, 200: time.Hour}
	for n, d := range want {
		until, _ := LockoutUntil(now, n)
		if until.Sub(now) != d {
			t.Errorf("%d fallos: %v, esperaba %v", n, until.Sub(now), d)
		}
	}
}

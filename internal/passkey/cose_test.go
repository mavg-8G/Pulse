package passkey

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"sort"
	"testing"
)

func TestClavesCOSE(t *testing.T) {
	msg := []byte("datos firmados")
	h := sha256.Sum256(msg)

	t.Run("EdDSA", func(t *testing.T) {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		k, err := parseCOSEKey(enc(map[any]any{1: 1, 3: -8, -1: 6, -2: []byte(pub)}))
		if err != nil || !k.verify(msg, ed25519.Sign(priv, msg)) || k.verify([]byte("otro"), ed25519.Sign(priv, msg)) {
			t.Fatal("EdDSA")
		}
	})
	t.Run("RS256", func(t *testing.T) {
		priv, _ := rsa.GenerateKey(rand.Reader, 2048)
		e := big.NewInt(int64(priv.E)).Bytes()
		k, err := parseCOSEKey(enc(map[any]any{1: 3, 3: -257, -1: priv.N.Bytes(), -2: e}))
		if err != nil {
			t.Fatal(err)
		}
		sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
		if !k.verify(msg, sig) {
			t.Fatal("RS256 válida rechazada")
		}
		sig[0] ^= 1
		if k.verify(msg, sig) {
			t.Fatal("RS256 alterada aceptada")
		}
	})
	t.Run("rechaza claves inválidas", func(t *testing.T) {
		ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		x := ec.X.FillBytes(make([]byte, 32))
		bad := []map[any]any{
			{1: 2, 3: -7, -1: 1, -2: x, -3: make([]byte, 32)},           // punto fuera de la curva
			{1: 2, 3: -7, -1: 2, -2: x, -3: x},                          // curva P-384 declarada
			{1: 2, 3: -7, -1: 1, -2: x[:31], -3: x},                     // longitud incorrecta
			{1: 1, 3: -8, -1: 6, -2: []byte{1, 2, 3}},                   // ed25519 corta
			{1: 3, 3: -257, -1: make([]byte, 128), -2: []byte{1, 0, 1}}, // RSA de 1024 bits
			{1: 3, 3: -257, -1: make([]byte, 256), -2: []byte{4}},       // exponente par
			{1: 2, 3: -35, -1: 1, -2: x, -3: x},                         // algoritmo no admitido
		}
		for i, m := range bad {
			if _, err := parseCOSEKey(enc(m)); err == nil {
				t.Errorf("caso %d aceptado", i)
			}
		}
		if _, err := parseCOSEKey([]byte{0x80}); err == nil {
			t.Error("no-mapa aceptado")
		}
	})
}

func TestCBORestricto(t *testing.T) {
	bad := map[string][]byte{
		"longitud indefinida": {0x9f, 0x01, 0xff},
		"flotante":            {0xfa, 0, 0, 0, 0},
		"tag":                 {0xc0, 0x01},
		"clave duplicada":     {0xa2, 0x01, 0x01, 0x01, 0x02},
		"clave bytes":         {0xa1, 0x41, 0x00, 0x01},
		"truncado":            {0x5a, 0, 0, 1, 0},
		"array enorme":        {0x9a, 0xff, 0xff, 0xff, 0xff},
		"vacío":               {},
	}
	for name, b := range bad {
		if _, _, err := decodeCBOR(b); err == nil {
			t.Errorf("%s: aceptado", name)
		}
	}
	deep := []byte{}
	for i := 0; i < 20; i++ {
		deep = append(deep, 0x81)
	}
	deep = append(deep, 0x01)
	if _, _, err := decodeCBOR(deep); err == nil {
		t.Error("anidamiento excesivo aceptado")
	}
	v, n, err := decodeCBOR([]byte{0xa3, 0x01, 0xf5, 0x02, 0xf4, 0x20, 0xf6})
	if err != nil || n != 7 || v.(map[any]any)[int64(-1)] != nil || v.(map[any]any)[int64(1)] != true {
		t.Fatalf("decodificación básica: %v %v", v, err)
	}
	for _, b := range [][]byte{{0x18, 0xff}, {0x19, 0xff, 0xff}, {0x1a, 0, 0, 0, 1}, {0x1b, 0, 0, 0, 0, 0, 0, 0, 1}} {
		if _, _, err := decodeCBOR(b); err != nil {
			t.Errorf("%x: %v", b, err)
		}
	}
}

// enc: codificador CBOR mínimo para tests (copia del de passkeytest, que no se puede importar aquí).
func enc(v any) []byte {
	head := func(major byte, n uint64) []byte {
		switch {
		case n < 24:
			return []byte{major<<5 | byte(n)}
		case n < 256:
			return []byte{major<<5 | 24, byte(n)}
		default:
			b := []byte{major<<5 | 25, 0, 0}
			binary.BigEndian.PutUint16(b[1:], uint16(n))
			return b
		}
	}
	switch x := v.(type) {
	case int:
		if x >= 0 {
			return head(0, uint64(x))
		}
		return head(1, uint64(-1-x))
	case []byte:
		return append(head(2, uint64(len(x))), x...)
	case map[any]any:
		keys := make([]any, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return bytes.Compare(enc(keys[i]), enc(keys[j])) < 0 })
		out := head(5, uint64(len(x)))
		for _, k := range keys {
			out = append(out, enc(k)...)
			out = append(out, enc(x[k])...)
		}
		return out
	}
	panic("tipo no soportado")
}

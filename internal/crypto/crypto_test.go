package crypto

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func ring(t testing.TB) *Keyring {
	k, err := NewKeyring(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestIdaYVuelta(t *testing.T) {
	k := ring(t)
	w := k.NewDEK("u1")
	s, err := k.Sealer("u1", w)
	if err != nil {
		t.Fatal(err)
	}
	ct := s.Seal("merchant", "Supermaxi Ñuñoa")
	if bytes.Contains(ct, []byte("Supermaxi")) {
		t.Fatal("el texto cifrado contiene el texto plano")
	}
	got, err := s.Open("merchant", ct)
	if err != nil || got != "Supermaxi Ñuñoa" {
		t.Fatalf("got %q %v", got, err)
	}
	if s.Seal("merchant", "") != nil {
		t.Fatal("vacío debe guardarse como nil")
	}
	if v, err := s.Open("merchant", nil); err != nil || v != "" {
		t.Fatal("nil debe abrir como vacío")
	}
}

func TestNonceAleatorio(t *testing.T) {
	k := ring(t)
	s, _ := k.Sealer("u1", k.NewDEK("u1"))
	if bytes.Equal(s.Seal("f", "x"), s.Seal("f", "x")) {
		t.Fatal("dos cifrados del mismo texto no deben ser iguales")
	}
}

func TestRechazaManipulacion(t *testing.T) {
	k := ring(t)
	w := k.NewDEK("u1")
	s, _ := k.Sealer("u1", w)
	ct := s.Seal("merchant", "Uber Eats")

	t.Run("byte alterado", func(t *testing.T) {
		for i := range ct {
			c := bytes.Clone(ct)
			c[i] ^= 1
			if _, err := s.Open("merchant", c); err == nil {
				t.Fatalf("alterar el byte %d no fue detectado", i)
			}
		}
	})
	t.Run("otro campo", func(t *testing.T) {
		if _, err := s.Open("notes", ct); err == nil {
			t.Fatal("abrir con otro nombre de campo debe fallar")
		}
	})
	t.Run("otro usuario", func(t *testing.T) {
		w2 := k.NewDEK("u2")
		s2, _ := k.Sealer("u2", w2)
		if _, err := s2.Open("merchant", ct); err == nil {
			t.Fatal("otro usuario no debe poder descifrar")
		}
	})
	t.Run("DEK de otro usuario", func(t *testing.T) {
		if _, err := k.Sealer("u2", w); err == nil {
			t.Fatal("la DEK de u1 no debe abrirse como si fuera de u2")
		}
	})
	t.Run("otra KEK", func(t *testing.T) {
		k2, _ := NewKeyring(bytes.Repeat([]byte{8}, 32))
		if _, err := k2.Sealer("u1", w); err == nil {
			t.Fatal("otra KEK no debe abrir la DEK")
		}
	})
	t.Run("truncado", func(t *testing.T) {
		for n := 0; n < len(ct); n++ {
			if _, err := s.Open("merchant", ct[:n]); err == nil && n > 0 {
				t.Fatalf("truncado a %d no fue detectado", n)
			}
		}
	})
}

func TestKEKInvalida(t *testing.T) {
	if _, err := NewKeyring(make([]byte, 16)); err == nil {
		t.Fatal("16 bytes debe fallar")
	}
	if _, err := NewKeyring(make([]byte, 32)); err == nil {
		t.Fatal("KEK de ceros debe fallar")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "kek")
	os.WriteFile(p, []byte("no-es-base64!!"), 0o600)
	if _, err := LoadKEKFile(p); err == nil {
		t.Fatal("archivo inválido debe fallar")
	}
	os.WriteFile(p, []byte(GenerateKEK()+"\n"), 0o600)
	if _, err := LoadKEKFile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKEKFile(filepath.Join(dir, "no-existe")); err == nil {
		t.Fatal("archivo inexistente debe fallar")
	}
	if len(base64.StdEncoding.EncodeToString(make([]byte, 32))) != len(GenerateKEK()) {
		t.Fatal("longitud inesperada")
	}
}

func TestLookupSeparaPropositos(t *testing.T) {
	k := ring(t)
	if bytes.Equal(k.Lookup("session", []byte("x")), k.Lookup("invite", []byte("x"))) {
		t.Fatal("propósitos distintos deben dar HMAC distintos")
	}
	if !Equal(k.Lookup("a", []byte("x")), k.Lookup("a", []byte("x"))) {
		t.Fatal("debe ser determinista")
	}
}

func FuzzOpen(f *testing.F) {
	k := ring(f)
	s, _ := k.Sealer("u1", k.NewDEK("u1"))
	f.Add(s.Seal("f", "hola"))
	f.Add([]byte{1})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		// Nunca debe entrar en pánico, y un dato aleatorio no debe "descifrar".
		if v, err := s.Open("f", b); err == nil && len(b) > 0 && v != "hola" {
			t.Fatalf("dato arbitrario descifró a %q", v)
		}
	})
}

func TestBytesYPimienta(t *testing.T) {
	k := ring(t)
	s, _ := k.Sealer("u1", k.NewDEK("u1"))
	ct := s.SealBytes("totp", []byte{1, 2, 3})
	if b, err := s.OpenBytes("totp", ct); err != nil || !bytes.Equal(b, []byte{1, 2, 3}) {
		t.Fatal("bytes")
	}
	if _, err := s.OpenBytes("otro", ct); err == nil {
		t.Fatal("campo distinto")
	}
	if bytes.Equal(k.Pepper("a"), k.Pepper("b")) || !bytes.Equal(k.Pepper("a"), k.Pepper("a")) {
		t.Fatal("pimienta")
	}
	if len(RandomBytes(5)) != 5 {
		t.Fatal("random")
	}
}

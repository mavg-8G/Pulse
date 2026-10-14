package passkey_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"pulse/internal/passkey"
	"pulse/internal/passkey/passkeytest"
)

var rp = passkey.RP{ID: "pulse.espectro.uk", Origin: "https://pulse.espectro.uk", Name: "Pulse"}

func register(t *testing.T, a *passkeytest.Authenticator) *passkey.Credential {
	t.Helper()
	ch := passkey.NewChallenge()
	c, err := rp.FinishRegistration(ch, a.Register(ch))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRegistroYLogin(t *testing.T) {
	a := passkeytest.New(rp.ID, rp.Origin)
	cred := register(t, a)
	if !bytes.Equal(cred.ID, a.CredID) || cred.Alg != passkey.AlgES256 {
		t.Fatal("credencial mal extraída")
	}
	ch := passkey.NewChallenge()
	_, count, err := rp.FinishLogin(ch, a.Login(ch), func(id []byte) (*passkey.Credential, error) { return cred, nil })
	if err != nil || count != 1 {
		t.Fatalf("login: %v count=%d", err, count)
	}
}

func TestRegistroRechaza(t *testing.T) {
	ch := passkey.NewChallenge()
	cases := map[string]func(a *passkeytest.Authenticator) *passkey.RegistrationResponse{
		"otro desafío": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			return a.Register(passkey.NewChallenge())
		},
		"otro origen": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			a.BadOrigin = "https://pulse.espectro.uk.evil.com"
			return a.Register(ch)
		},
		"otro rpId": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			a.RPID = "evil.com"
			return a.Register(ch)
		},
		"sin presencia": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			a.NoUserPresence = true
			return a.Register(ch)
		},
		"rawId distinto": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			r := a.Register(ch)
			r.RawID = []byte("otro")
			return r
		},
		"tipo get en vez de create": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			r := a.Register(ch)
			l := a.Login(ch)
			r.Response.ClientDataJSON = l.Response.ClientDataJSON
			return r
		},
		"attestation basura": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			r := a.Register(ch)
			r.Response.AttestationObject = []byte{0xa1, 0x01}
			return r
		},
		"bytes sobrantes": func(a *passkeytest.Authenticator) *passkey.RegistrationResponse {
			r := a.Register(ch)
			r.Response.AttestationObject = append(r.Response.AttestationObject, 0)
			return r
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			a := passkeytest.New(rp.ID, rp.Origin)
			if _, err := rp.FinishRegistration(ch, mk(a)); err == nil {
				t.Fatal("debió rechazarse")
			}
		})
	}
}

func TestLoginRechaza(t *testing.T) {
	a := passkeytest.New(rp.ID, rp.Origin)
	cred := register(t, a)
	look := func(id []byte) (*passkey.Credential, error) {
		if bytes.Equal(id, cred.ID) {
			c := *cred
			return &c, nil
		}
		return nil, passkey.ErrUnknown
	}

	t.Run("firma alterada", func(t *testing.T) {
		ch := passkey.NewChallenge()
		r := a.Login(ch)
		r.Response.Signature[len(r.Response.Signature)-1] ^= 1
		if _, _, err := rp.FinishLogin(ch, r, look); err == nil {
			t.Fatal("firma alterada aceptada")
		}
	})
	t.Run("authData alterado", func(t *testing.T) {
		ch := passkey.NewChallenge()
		r := a.Login(ch)
		r.Response.AuthenticatorData[36] ^= 0xff
		if _, _, err := rp.FinishLogin(ch, r, look); err == nil {
			t.Fatal("authData alterado aceptado")
		}
	})
	t.Run("desafío reutilizado de otra sesión", func(t *testing.T) {
		r := a.Login(passkey.NewChallenge())
		if _, _, err := rp.FinishLogin(passkey.NewChallenge(), r, look); err == nil {
			t.Fatal("desafío distinto aceptado")
		}
	})
	t.Run("credencial de otro usuario", func(t *testing.T) {
		other := passkeytest.New(rp.ID, rp.Origin)
		register(t, other)
		ch := passkey.NewChallenge()
		if _, _, err := rp.FinishLogin(ch, other.Login(ch), look); err != passkey.ErrUnknown {
			t.Fatalf("esperaba ErrUnknown, obtuve %v", err)
		}
	})
	t.Run("contador que no avanza (clon)", func(t *testing.T) {
		c := *cred
		c.SignCount = 1000
		ch := passkey.NewChallenge()
		_, _, err := rp.FinishLogin(ch, a.Login(ch), func([]byte) (*passkey.Credential, error) { return &c, nil })
		if err != passkey.ErrCounter {
			t.Fatalf("esperaba ErrCounter, obtuve %v", err)
		}
	})
	t.Run("sin presencia del usuario", func(t *testing.T) {
		b := passkeytest.New(rp.ID, rp.Origin)
		cb := register(t, b)
		b.NoUserPresence = true
		ch := passkey.NewChallenge()
		if _, _, err := rp.FinishLogin(ch, b.Login(ch), func([]byte) (*passkey.Credential, error) { return cb, nil }); err == nil {
			t.Fatal("sin UP aceptado")
		}
	})
}

func TestOpcionesJSON(t *testing.T) {
	o := rp.CreationOptions([]byte{1, 2, 3}, []byte("uid"), "a@b.com", [][]byte{{9}})
	b, _ := json.Marshal(o)
	for _, want := range []string{`"challenge":"AQID"`, `"attestation":"none"`, `"id":"pulse.espectro.uk"`, `"alg":-7`, `"excludeCredentials":[{"type":"public-key","id":"CQ"}]`} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("falta %s en %s", want, b)
		}
	}
}

func FuzzCOSEyAuthData(f *testing.F) {
	a := passkeytest.New(rp.ID, rp.Origin)
	ch := passkey.NewChallenge()
	f.Add([]byte(a.Register(ch).Response.AttestationObject))
	f.Add([]byte{0xbf})
	f.Fuzz(func(t *testing.T, att []byte) {
		r := a.Register(ch)
		r.Response.AttestationObject = att
		rp.FinishRegistration(ch, r) // no debe entrar en pánico
	})
}

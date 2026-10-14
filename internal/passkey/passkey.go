// Package passkey implementa la verificación de WebAuthn (passkeys) del lado del
// servidor con la biblioteca estándar: registro y autenticación, sin atestación
// (política "none": Pulse confía en la passkey porque se registra dentro de una
// sesión ya autenticada, no por el fabricante del dispositivo).
package passkey

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	pcrypto "pulse/internal/crypto"
)

var b64 = base64.RawURLEncoding

// B64 es []byte que se serializa como base64url sin relleno (formato WebAuthn JSON).
type B64 []byte

func (b B64) MarshalJSON() ([]byte, error) { return json.Marshal(b64.EncodeToString(b)) }

func (b *B64) UnmarshalJSON(d []byte) error {
	var s string
	if err := json.Unmarshal(d, &s); err != nil {
		return err
	}
	// Algunos navegadores antiguos añaden relleno "=".
	v, err := b64.DecodeString(stringsTrimRight(s, "="))
	if err != nil {
		return err
	}
	*b = v
	return nil
}

func stringsTrimRight(s, cut string) string {
	for len(s) > 0 && s[len(s)-1] == cut[0] {
		s = s[:len(s)-1]
	}
	return s
}

type RP struct {
	ID     string // host, p. ej. pulse.espectro.uk
	Origin string // https://pulse.espectro.uk
	Name   string
}

type CredParam struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

type CredDescriptor struct {
	Type string `json:"type"`
	ID   B64    `json:"id"`
}

type CreationOptions struct {
	Challenge B64 `json:"challenge"`
	RP        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"rp"`
	User struct {
		ID          B64    `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"user"`
	PubKeyCredParams       []CredParam      `json:"pubKeyCredParams"`
	Timeout                int              `json:"timeout"`
	Attestation            string           `json:"attestation"`
	ExcludeCredentials     []CredDescriptor `json:"excludeCredentials"`
	AuthenticatorSelection struct {
		ResidentKey      string `json:"residentKey"`
		UserVerification string `json:"userVerification"`
	} `json:"authenticatorSelection"`
}

type RequestOptions struct {
	Challenge        B64              `json:"challenge"`
	RPID             string           `json:"rpId"`
	Timeout          int              `json:"timeout"`
	UserVerification string           `json:"userVerification"`
	AllowCredentials []CredDescriptor `json:"allowCredentials"`
}

// RegistrationResponse / AssertionResponse: formato de PublicKeyCredential.toJSON().
type RegistrationResponse struct {
	ID       string `json:"id"`
	RawID    B64    `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		ClientDataJSON    B64 `json:"clientDataJSON"`
		AttestationObject B64 `json:"attestationObject"`
	} `json:"response"`
}

type AssertionResponse struct {
	ID       string `json:"id"`
	RawID    B64    `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		ClientDataJSON    B64 `json:"clientDataJSON"`
		AuthenticatorData B64 `json:"authenticatorData"`
		Signature         B64 `json:"signature"`
		UserHandle        B64 `json:"userHandle"`
	} `json:"response"`
}

// Credential es lo que se guarda tras un registro válido.
type Credential struct {
	ID        []byte
	PublicKey []byte // COSE
	Alg       int64
	SignCount uint32
}

var (
	ErrClientData = errors.New("passkey: datos del cliente inválidos")
	ErrAuthData   = errors.New("passkey: datos del autenticador inválidos")
	ErrSignature  = errors.New("passkey: firma inválida")
	ErrCounter    = errors.New("passkey: contador de firmas no avanzó (posible clon)")
	ErrUnknown    = errors.New("passkey: credencial desconocida")
)

func NewChallenge() []byte { return pcrypto.RandomBytes(32) }

func (rp RP) CreationOptions(challenge, userHandle []byte, name string, exclude [][]byte) CreationOptions {
	o := CreationOptions{Challenge: challenge, Timeout: 120000, Attestation: "none"}
	o.RP.ID, o.RP.Name = rp.ID, rp.Name
	o.User.ID, o.User.Name, o.User.DisplayName = userHandle, name, name
	o.PubKeyCredParams = []CredParam{{"public-key", AlgES256}, {"public-key", AlgEdDSA}, {"public-key", AlgRS256}}
	o.AuthenticatorSelection.ResidentKey = "preferred"
	o.AuthenticatorSelection.UserVerification = "preferred"
	o.ExcludeCredentials = []CredDescriptor{}
	for _, id := range exclude {
		o.ExcludeCredentials = append(o.ExcludeCredentials, CredDescriptor{"public-key", id})
	}
	return o
}

func (rp RP) RequestOptions(challenge []byte, allow [][]byte) RequestOptions {
	o := RequestOptions{Challenge: challenge, RPID: rp.ID, Timeout: 120000, UserVerification: "preferred", AllowCredentials: []CredDescriptor{}}
	for _, id := range allow {
		o.AllowCredentials = append(o.AllowCredentials, CredDescriptor{"public-key", id})
	}
	return o
}

type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

func (rp RP) checkClientData(raw []byte, wantType string, challenge []byte) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return ErrClientData
	}
	got, err := b64.DecodeString(stringsTrimRight(cd.Challenge, "="))
	if err != nil || cd.Type != wantType || cd.CrossOrigin || cd.Origin != rp.Origin ||
		len(challenge) == 0 || subtle.ConstantTimeCompare(got, challenge) != 1 {
		return ErrClientData
	}
	return nil
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
	flagED = 0x80
)

type authData struct {
	flags     byte
	signCount uint32
	credID    []byte
	cose      []byte
}

func (rp RP) parseAuthData(b []byte, wantCred bool) (*authData, error) {
	if len(b) < 37 {
		return nil, ErrAuthData
	}
	rpHash := sha256.Sum256([]byte(rp.ID))
	if subtle.ConstantTimeCompare(b[:32], rpHash[:]) != 1 {
		return nil, ErrAuthData
	}
	ad := &authData{flags: b[32], signCount: binary.BigEndian.Uint32(b[33:37])}
	if ad.flags&flagUP == 0 {
		return nil, ErrAuthData // el usuario debe estar presente
	}
	rest := b[37:]
	if wantCred {
		if ad.flags&flagAT == 0 || len(rest) < 18 {
			return nil, ErrAuthData
		}
		l := int(binary.BigEndian.Uint16(rest[16:18]))
		rest = rest[18:]
		if l == 0 || l > 1023 || len(rest) < l {
			return nil, ErrAuthData
		}
		ad.credID = append([]byte(nil), rest[:l]...)
		rest = rest[l:]
		_, n, err := decodeCBOR(rest)
		if err != nil {
			return nil, ErrAuthData
		}
		ad.cose = append([]byte(nil), rest[:n]...)
		rest = rest[n:]
	} else if ad.flags&flagAT != 0 {
		return nil, ErrAuthData
	}
	if ad.flags&flagED != 0 {
		_, n, err := decodeCBOR(rest)
		if err != nil {
			return nil, ErrAuthData
		}
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return nil, ErrAuthData
	}
	return ad, nil
}

// FinishRegistration verifica la respuesta de navigator.credentials.create().
func (rp RP) FinishRegistration(challenge []byte, r *RegistrationResponse) (*Credential, error) {
	if r.Type != "public-key" {
		return nil, ErrClientData
	}
	if err := rp.checkClientData(r.Response.ClientDataJSON, "webauthn.create", challenge); err != nil {
		return nil, err
	}
	v, n, err := decodeCBOR(r.Response.AttestationObject)
	if err != nil || n != len(r.Response.AttestationObject) {
		return nil, ErrAuthData
	}
	att, ok := v.(map[any]any)
	if !ok {
		return nil, ErrAuthData
	}
	fmtName, _ := att["fmt"].(string)
	raw, _ := att["authData"].([]byte)
	if _, ok := att["attStmt"].(map[any]any); !ok || fmtName == "" {
		return nil, ErrAuthData
	}
	ad, err := rp.parseAuthData(raw, true)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(ad.credID, r.RawID) {
		return nil, ErrAuthData
	}
	pk, err := parseCOSEKey(ad.cose)
	if err != nil {
		return nil, err
	}
	return &Credential{ID: ad.credID, PublicKey: ad.cose, Alg: pk.alg, SignCount: ad.signCount}, nil
}

// FinishLogin verifica navigator.credentials.get(). lookup busca la credencial
// SOLO entre las del usuario esperado. Devuelve el nuevo contador.
func (rp RP) FinishLogin(challenge []byte, r *AssertionResponse, lookup func(id []byte) (*Credential, error)) (*Credential, uint32, error) {
	if r.Type != "public-key" || len(r.RawID) == 0 {
		return nil, 0, ErrClientData
	}
	cred, err := lookup(r.RawID)
	if err != nil || cred == nil {
		return nil, 0, ErrUnknown
	}
	if err := rp.checkClientData(r.Response.ClientDataJSON, "webauthn.get", challenge); err != nil {
		return nil, 0, err
	}
	ad, err := rp.parseAuthData(r.Response.AuthenticatorData, false)
	if err != nil {
		return nil, 0, err
	}
	pk, err := parseCOSEKey(cred.PublicKey)
	if err != nil {
		return nil, 0, err
	}
	cdHash := sha256.Sum256(r.Response.ClientDataJSON)
	signed := append(append([]byte(nil), r.Response.AuthenticatorData...), cdHash[:]...)
	if !pk.verify(signed, r.Response.Signature) {
		return nil, 0, ErrSignature
	}
	if (ad.signCount != 0 || cred.SignCount != 0) && ad.signCount <= cred.SignCount {
		return nil, 0, ErrCounter
	}
	return cred, ad.signCount, nil
}

func (c *Credential) String() string { return fmt.Sprintf("cred(%x…)", c.ID[:min(4, len(c.ID))]) }

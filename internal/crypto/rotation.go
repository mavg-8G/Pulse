package crypto

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

// Key bundles separate wrapping/server keys from lookup/pepper. The latter stay
// stable during a wrapping-key rotation so passwords and opaque indexes survive.
// This is maintenance rotation, not complete recovery from a compromised KEK.
type keyBundle struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Wrap    []byte `json:"wrap"`
	Server  []byte `json:"server"`
	Lookup  []byte `json:"lookup"`
	Pepper  []byte `json:"pepper"`
}

func loadBundle(raw []byte) (*Keyring, error) {
	var b keyBundle
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return nil, errors.New("llavero inválido")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("llavero inválido")
	}
	id, idErr := base64.RawURLEncoding.DecodeString(b.ID)
	if b.Version != 2 || len(b.ID) != 22 || idErr != nil || len(id) != 16 {
		return nil, errors.New("versión o identificador del llavero inválido")
	}
	for _, key := range [][]byte{b.Wrap, b.Server, b.Lookup, b.Pepper} {
		if len(key) != KeySize || bytes.Equal(key, make([]byte, KeySize)) {
			return nil, errors.New("clave del llavero inválida")
		}
	}
	return &Keyring{wrap: newGCM(b.Wrap), lookup: b.Lookup, pepper: b.Pepper, server: &Sealer{g: newGCM(b.Server), userID: "server"}}, nil
}

// RotateBundle produces new wrapping and server keys; never writes or activates them.
func (k *Keyring) RotateBundle() (*Keyring, []byte, error) {
	b := keyBundle{Version: 2, ID: base64.RawURLEncoding.EncodeToString(RandomBytes(16)), Wrap: RandomBytes(KeySize), Server: RandomBytes(KeySize), Lookup: k.lookup, Pepper: k.pepper}
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, nil, err
	}
	next, err := loadBundle(raw)
	return next, raw, err
}

func (k *Keyring) RewrapDEK(userID string, wrapped []byte, next *Keyring) ([]byte, error) {
	dek, err := open(k.wrap, wrapped, []byte("dek|"+userID))
	if err != nil {
		return nil, err
	}
	defer clear(dek)
	return seal(next.wrap, dek, []byte("dek|"+userID)), nil
}

package passkey

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"math/big"
)

// Algoritmos COSE admitidos.
const (
	AlgES256 = -7
	AlgEdDSA = -8
	AlgRS256 = -257
)

var errKey = errors.New("clave pública no admitida")

type publicKey struct {
	alg int64
	key crypto.PublicKey
}

func mapInt(m map[any]any, k int64) (int64, bool) {
	v, ok := m[k].(int64)
	return v, ok
}

func mapBytes(m map[any]any, k int64) ([]byte, bool) {
	v, ok := m[k].([]byte)
	return v, ok
}

// parseCOSEKey valida y convierte una clave COSE (RFC 9053).
func parseCOSEKey(b []byte) (*publicKey, error) {
	v, n, err := decodeCBOR(b)
	if err != nil || n != len(b) {
		return nil, errKey
	}
	m, ok := v.(map[any]any)
	if !ok {
		return nil, errKey
	}
	kty, _ := mapInt(m, 1)
	alg, _ := mapInt(m, 3)
	switch alg {
	case AlgES256:
		crv, _ := mapInt(m, -1)
		x, okx := mapBytes(m, -2)
		y, oky := mapBytes(m, -3)
		if kty != 2 || crv != 1 || !okx || !oky || len(x) != 32 || len(y) != 32 {
			return nil, errKey
		}
		raw := append(append([]byte{4}, x...), y...)
		if _, err := ecdh.P256().NewPublicKey(raw); err != nil { // valida que el punto esté en la curva
			return nil, errKey
		}
		return &publicKey{alg, &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}}, nil
	case AlgEdDSA:
		crv, _ := mapInt(m, -1)
		x, okx := mapBytes(m, -2)
		if kty != 1 || crv != 6 || !okx || len(x) != ed25519.PublicKeySize {
			return nil, errKey
		}
		return &publicKey{alg, ed25519.PublicKey(x)}, nil
	case AlgRS256:
		nb, okn := mapBytes(m, -1)
		eb, oke := mapBytes(m, -2)
		if kty != 3 || !okn || !oke || len(nb) < 256 || len(eb) == 0 || len(eb) > 4 {
			return nil, errKey
		}
		e := int(new(big.Int).SetBytes(eb).Int64())
		if e < 3 || e%2 == 0 {
			return nil, errKey
		}
		return &publicKey{alg, &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}}, nil
	}
	return nil, errKey
}

func (p *publicKey) verify(data, sig []byte) bool {
	switch k := p.key.(type) {
	case *ecdsa.PublicKey:
		h := sha256.Sum256(data)
		return ecdsa.VerifyASN1(k, h[:], sig)
	case ed25519.PublicKey:
		return ed25519.Verify(k, data, sig)
	case *rsa.PublicKey:
		h := sha256.Sum256(data)
		return rsa.VerifyPKCS1v15(k, crypto.SHA256, h[:], sig) == nil
	}
	return false
}

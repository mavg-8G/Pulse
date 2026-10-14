package passkey

import (
	"encoding/binary"
	"errors"
	"math"
)

// Decodificador CBOR (RFC 8949) mínimo y estricto: solo lo necesario para
// attestationObject y claves COSE. Rechaza tipos indefinidos, flotantes y tags.

var errCBOR = errors.New("cbor inválido")

const maxDepth = 8
const maxItems = 1024

type cborDecoder struct {
	b []byte
	i int
}

func (d *cborDecoder) head() (major byte, arg uint64, err error) {
	if d.i >= len(d.b) {
		return 0, 0, errCBOR
	}
	ib := d.b[d.i]
	d.i++
	major, info := ib>>5, ib&0x1f
	switch {
	case info < 24:
		return major, uint64(info), nil
	case info == 24:
		if d.i+1 > len(d.b) {
			return 0, 0, errCBOR
		}
		arg = uint64(d.b[d.i])
		d.i++
	case info == 25:
		if d.i+2 > len(d.b) {
			return 0, 0, errCBOR
		}
		arg = uint64(binary.BigEndian.Uint16(d.b[d.i:]))
		d.i += 2
	case info == 26:
		if d.i+4 > len(d.b) {
			return 0, 0, errCBOR
		}
		arg = uint64(binary.BigEndian.Uint32(d.b[d.i:]))
		d.i += 4
	case info == 27:
		if d.i+8 > len(d.b) {
			return 0, 0, errCBOR
		}
		arg = binary.BigEndian.Uint64(d.b[d.i:])
		d.i += 8
	default:
		return 0, 0, errCBOR // longitudes indefinidas no se aceptan
	}
	return major, arg, nil
}

// value decodifica un elemento: int64, []byte, string, []any, map[any]any, bool, nil.
func (d *cborDecoder) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, errCBOR
	}
	major, arg, err := d.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case 0:
		if arg > math.MaxInt64 {
			return nil, errCBOR
		}
		return int64(arg), nil
	case 1:
		if arg > math.MaxInt64 {
			return nil, errCBOR
		}
		return -1 - int64(arg), nil
	case 2, 3:
		if arg > uint64(len(d.b)-d.i) {
			return nil, errCBOR
		}
		s := d.b[d.i : d.i+int(arg)]
		d.i += int(arg)
		if major == 2 {
			return append([]byte(nil), s...), nil
		}
		return string(s), nil
	case 4:
		if arg > maxItems {
			return nil, errCBOR
		}
		out := make([]any, 0, arg)
		for k := uint64(0); k < arg; k++ {
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case 5:
		if arg > maxItems {
			return nil, errCBOR
		}
		out := make(map[any]any, arg)
		for k := uint64(0); k < arg; k++ {
			key, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			switch key.(type) {
			case int64, string:
			default:
				return nil, errCBOR
			}
			if _, dup := out[key]; dup {
				return nil, errCBOR
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out[key] = v
		}
		return out, nil
	case 7:
		switch arg {
		case 20:
			return false, nil
		case 21:
			return true, nil
		case 22:
			return nil, nil
		}
	}
	return nil, errCBOR
}

// decodeCBOR decodifica un único elemento y devuelve cuántos bytes consumió.
func decodeCBOR(b []byte) (any, int, error) {
	d := &cborDecoder{b: b}
	v, err := d.value(0)
	if err != nil {
		return nil, 0, err
	}
	return v, d.i, nil
}

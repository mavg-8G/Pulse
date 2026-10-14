// Package mailparse lee correos de notificación bancaria y extrae el gasto.
//
//	.eml ─► MIME (multipart, base64, quoted-printable, charsets) ─► HTML → texto (una celda por línea)
//	     ─► parser del banco (por remitente y frases clave) ─► Expense normalizado
//
// Nunca se adivina: si una plantilla no se reconoce, el resultado es "unrecognized",
// no un gasto inventado.
package mailparse

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const maxEmail = 2 << 20 // 2 MiB: ninguna notificación bancaria pesa más

// Email es la versión normalizada de un correo.
type Email struct {
	From      string // dirección en minúsculas
	FromName  string
	Subject   string
	MessageID string
	Date      time.Time
	Text      string // cuerpo como texto plano, una celda/párrafo por línea
}

var ErrTooLarge = errors.New("correo demasiado grande")

// ParseEML lee un .eml completo.
func ParseEML(r io.Reader) (*Email, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxEmail+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxEmail {
		return nil, ErrTooLarge
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("no es un correo válido: %w", err)
	}
	dec := &mime.WordDecoder{CharsetReader: charsetReader}
	e := &Email{MessageID: strings.Trim(msg.Header.Get("Message-Id"), "<> ")}
	if subj, err := dec.DecodeHeader(msg.Header.Get("Subject")); err == nil {
		e.Subject = strings.TrimSpace(subj)
	} else {
		e.Subject = msg.Header.Get("Subject")
	}
	if from, err := (&mail.AddressParser{WordDecoder: dec}).Parse(msg.Header.Get("From")); err == nil {
		e.From, e.FromName = strings.ToLower(from.Address), from.Name
	}
	if d, err := msg.Header.Date(); err == nil {
		e.Date = d
	}
	html, plain, err := walk(textproto(msg.Header), msg.Body, 0)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(html) != "" {
		e.Text = HTMLToText(html)
	} else {
		e.Text = normalizeText(plain)
	}
	return e, nil
}

type header interface{ Get(string) string }

type textproto mail.Header

func (h textproto) Get(k string) string { return mail.Header(h).Get(k) }

// walk recorre partes MIME (hasta 5 niveles) y devuelve el HTML y el texto plano.
func walk(h header, body io.Reader, depth int) (html, plain string, err error) {
	if depth > 5 {
		return "", "", errors.New("MIME demasiado anidado")
	}
	ct := h.Get("Content-Type")
	if ct == "" {
		ct = "text/plain; charset=utf-8"
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mt, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return html, plain, err
			}
			h2, p2, err := walk(p.Header, p, depth+1)
			if err != nil {
				return html, plain, err
			}
			if html == "" {
				html = h2
			}
			if plain == "" {
				plain = p2
			}
		}
		return html, plain, nil
	}
	if mt != "text/html" && mt != "text/plain" {
		return "", "", nil // adjuntos e imágenes se ignoran
	}
	var r io.Reader = body
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, &stripNewlines{r: body})
	case "quoted-printable":
		r = quotedprintable.NewReader(body)
	}
	b, err := io.ReadAll(io.LimitReader(r, maxEmail))
	if err != nil {
		return "", "", fmt.Errorf("decodificando el cuerpo: %w", err)
	}
	s, err := toUTF8(params["charset"], b)
	if err != nil {
		return "", "", err
	}
	if mt == "text/html" {
		return s, "", nil
	}
	return "", s, nil
}

type stripNewlines struct{ r io.Reader }

func (s *stripNewlines) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for i := 0; i < n; i++ {
		if p[i] != '\r' && p[i] != '\n' && p[i] != ' ' && p[i] != '\t' {
			p[j] = p[i]
			j++
		}
	}
	return j, err
}

// cp1252 para 0x80–0x9F (el resto coincide con ISO-8859-1).
var cp1252 = map[byte]rune{0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡', 0x88: 'ˆ', 0x89: '‰',
	0x8A: 'Š', 0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž', 0x91: '\'', 0x92: '\'', 0x93: '"', 0x94: '"', 0x95: '•', 0x96: '–', 0x97: '—',
	0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ', 0x9E: 'ž', 0x9F: 'Ÿ'}

func toUTF8(charset string, b []byte) (string, error) {
	switch strings.ToLower(strings.Trim(charset, `"' `)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		if !utf8.Valid(b) {
			return strings.ToValidUTF8(string(b), "�"), nil
		}
		return string(b), nil
	case "iso-8859-1", "latin1", "iso8859-1", "iso-8859-15":
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r), nil
	case "windows-1252", "cp1252":
		var sb strings.Builder
		for _, c := range b {
			if x, ok := cp1252[c]; ok {
				sb.WriteRune(x)
			} else {
				sb.WriteRune(rune(c))
			}
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("juego de caracteres no soportado: %s", charset)
}

func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(io.LimitReader(input, 64<<10))
	if err != nil {
		return nil, err
	}
	s, err := toUTF8(charset, b)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(s), nil
}

package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"unicode"

	"pulse/internal/store"
)

// apiError es un error con estado HTTP y mensaje apto para mostrar al usuario.
type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func errf(status int, code, msg string) *apiError { return &apiError{status, code, msg} }

func badRequest(msg string) *apiError { return errf(400, "invalid", msg) }

var (
	errUnauthorized = errf(401, "unauthorized", "Inicia sesión para continuar.")
	errNotFound     = errf(404, "not_found", "No encontrado.")
	errRateLimited  = errf(429, "rate_limited", "Demasiados intentos. Espera un momento y vuelve a intentar.")
	errStepUp       = errf(403, "step_up_required", "Confirma tu identidad para continuar.")
	errCSRF         = errf(403, "csrf", "Petición rechazada por origen no válido.")
)

type handler func(w http.ResponseWriter, r *http.Request) error

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func (s *Server) h(fn handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			s.fail(w, r, err)
		}
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	var ce *store.ConflictError
	switch {
	case errors.As(err, &ae):
	case errors.Is(err, store.ErrNotFound):
		ae = errNotFound
	case errors.As(err, &ce):
		ae = errf(409, "conflict", ce.Msg)
	default:
		s.log.Error("error interno", slog.String("path", r.URL.Path), slog.String("err", err.Error()))
		ae = errf(500, "internal", "Ocurrió un error inesperado.")
	}
	writeJSON(w, ae.Status, map[string]any{"error": ae})
}

const maxBody = 64 << 10

// decode exige JSON, limita el tamaño y rechaza campos desconocidos.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/json" {
		return errf(415, "unsupported_media_type", "Se esperaba JSON.")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errf(413, "too_large", "La petición es demasiado grande.")
		}
		return badRequest("JSON inválido: " + err.Error())
	}
	if _, err := dec.Token(); err != io.EOF {
		return badRequest("JSON inválido: datos extra al final.")
	}
	return nil
}

// cleanText quita caracteres de control y espacios sobrantes; valida longitud en runas.
func cleanText(s string, max int, field string) (string, error) {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, s))
	if n := len([]rune(s)); n > max {
		return "", badRequest(field + " es demasiado largo (máx. " + itoa(max) + " caracteres).")
	}
	return s, nil
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

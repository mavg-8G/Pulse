package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type ctxKey int

const (
	keyIP ctxKey = iota
	keyIdentity
)

func clientIP(r *http.Request) string {
	if v, ok := r.Context().Value(keyIP).(string); ok {
		return v
	}
	return ""
}

// realIP solo confía en X-Forwarded-For cuando la conexión viene de un proxy
// configurado (Caddy en la red "proxy"); en cualquier otro caso usa la IP de la conexión.
func (s *Server) realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := host
		if s.trusted(net.ParseIP(host)) {
			parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
			for i := len(parts) - 1; i >= 0; i-- {
				p := net.ParseIP(strings.TrimSpace(parts[i]))
				if p == nil {
					break
				}
				ip = p.String()
				if !s.trusted(p) {
					break
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyIP, ip)))
	})
}

func (s *Server) trusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range s.cfg.TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; font-src 'self'; " +
	"connect-src 'self'; manifest-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if s.secure {
			h.Set("Content-Security-Policy", csp+"; upgrade-insecure-requests")
		} else {
			h.Set("Content-Security-Policy", csp)
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if s.secure {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// csrf: toda petición que modifica algo debe venir del propio origen.
// Se usa Sec-Fetch-Site (navegadores modernos) y Origin como respaldo; si no hay ninguno se rechaza.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		// Pub/Sub no es un navegador ni usa cookies: se autentica con el JWT de Google.
		if r.Method == http.MethodPost && r.URL.Path == "/api/gmail/webhook" {
			next.ServeHTTP(w, r)
			return
		}
		sfs, origin := r.Header.Get("Sec-Fetch-Site"), r.Header.Get("Origin")
		ok := sfs != "" || origin != ""
		if sfs != "" && sfs != "same-origin" && sfs != "none" {
			ok = false
		}
		if origin != "" && origin != s.cfg.Origin() {
			ok = false
		}
		if !ok {
			s.fail(w, r, errCSRF)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// logRequests registra método, ruta (sin query: puede tener búsquedas), estado y duración.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("pánico", slog.Any("rec", rec), slog.String("stack", string(debug.Stack())))
				s.fail(sw, r, errf(500, "internal", "Ocurrió un error inesperado."))
			}
			if r.URL.Path == "/healthz" {
				return
			}
			s.log.Info("http", slog.String("m", r.Method), slog.String("p", r.URL.Path), slog.Int("s", sw.status),
				slog.Duration("d", time.Since(start)), slog.String("ip", clientIP(r)))
		}()
		next.ServeHTTP(sw, r)
	})
}

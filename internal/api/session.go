package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"pulse/internal/auth"
	"pulse/internal/crypto"
	"pulse/internal/store"
)

const (
	partialTTL      = 10 * time.Minute    // sesión a medio autenticar (falta el 2.º factor)
	idleTTL         = 12 * time.Hour      // navegador común
	rememberIdleTTL = 7 * 24 * time.Hour  // dispositivo recordado (PWA)
	absoluteTTL     = 30 * 24 * time.Hour // máximo absoluto, aunque haya actividad
	stepUpTTL       = 5 * time.Minute
	touchEvery      = time.Minute
)

type identity struct {
	sess   *store.Session
	user   *store.User
	sealer *crypto.Sealer
}

func ident(r *http.Request) *identity {
	id, _ := r.Context().Value(keyIdentity).(*identity)
	return id
}

func (id *identity) seal() *crypto.Sealer { return id.sealer }

func (s *Server) cookieName() string {
	if s.secure {
		return "__Host-pulse" // exige Secure, Path=/ y sin Domain: no se puede inyectar desde subdominios
	}
	return "pulse_session"
}

func (s *Server) setCookie(w http.ResponseWriter, token string, se *store.Session) {
	c := &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode}
	if se.Remember && se.State == "full" {
		c.Expires = se.AbsoluteExpiresAt
	}
	http.SetCookie(w, c)
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: "", Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// newSession crea una sesión con token aleatorio; en la BD solo queda su HMAC.
func (s *Server) newSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID, state string, remember bool) (*store.Session, error) {
	now := s.clock.Now()
	se := &store.Session{
		ID: auth.NewID(), UserID: userID, State: state, Remember: remember,
		IP: clientIP(r), UserAgent: truncate(r.UserAgent(), 200),
		CreatedAt: now, LastSeenAt: now, AbsoluteExpiresAt: now.Add(absoluteTTL),
	}
	switch {
	case state != "full":
		se.ExpiresAt, se.AbsoluteExpiresAt = now.Add(partialTTL), now.Add(partialTTL)
	case remember:
		se.ExpiresAt = now.Add(rememberIdleTTL)
	default:
		se.ExpiresAt = now.Add(idleTTL)
	}
	token := auth.Token()
	if err := s.store.CreateSession(ctx, se, s.keys.Lookup("session", []byte(token))); err != nil {
		return nil, err
	}
	s.setCookie(w, token, se)
	return se, nil
}

// upgrade completa la autenticación: borra la sesión parcial y emite un token NUEVO
// (rotación: un token robado antes del 2.º factor no sirve después).
func (s *Server) upgrade(ctx context.Context, w http.ResponseWriter, r *http.Request, id *identity) error {
	if err := s.store.DeleteSession(ctx, id.user.ID, id.sess.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	se, err := s.newSession(ctx, w, r, id.user.ID, "full", id.sess.Remember)
	if err != nil {
		return err
	}
	id.sess = se
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// loadIdentity valida la cookie. Devuelve nil si no hay sesión válida.
func (s *Server) loadIdentity(r *http.Request) (*identity, error) {
	c, err := r.Cookie(s.cookieName())
	if err != nil || c.Value == "" || len(c.Value) > 100 {
		return nil, nil
	}
	ctx := r.Context()
	se, err := s.store.SessionByToken(ctx, s.keys.Lookup("session", []byte(c.Value)))
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	if !now.Before(se.ExpiresAt) || !now.Before(se.AbsoluteExpiresAt) {
		s.store.DeleteSession(ctx, se.UserID, se.ID)
		return nil, nil
	}
	u, err := s.store.User(ctx, se.UserID)
	if err != nil {
		return nil, err
	}
	if u.DisabledAt != nil {
		s.store.DeleteSession(ctx, u.ID, se.ID)
		return nil, nil
	}
	if se.State == "full" && now.Sub(se.LastSeenAt) >= touchEvery {
		ttl := idleTTL
		if se.Remember {
			ttl = rememberIdleTTL
		}
		exp := now.Add(ttl)
		if exp.After(se.AbsoluteExpiresAt) {
			exp = se.AbsoluteExpiresAt
		}
		if err := s.store.TouchSession(ctx, se.ID, now, exp); err != nil {
			return nil, err
		}
		se.LastSeenAt, se.ExpiresAt = now, exp
	}
	sl, err := s.keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		return nil, err
	}
	return &identity{sess: se, user: u, sealer: sl}, nil
}

// authed exige una sesión en alguno de los estados indicados.
func (s *Server) authed(states []string, fn handler) http.HandlerFunc {
	return s.h(func(w http.ResponseWriter, r *http.Request) error {
		id, err := s.loadIdentity(r)
		if err != nil {
			return err
		}
		if id == nil {
			s.clearCookie(w)
			return errUnauthorized
		}
		allowed := false
		for _, st := range states {
			if id.sess.State == st {
				allowed = true
			}
		}
		if !allowed {
			switch id.sess.State {
			case "mfa":
				return errf(401, "mfa_required", "Falta verificar tu segundo factor.")
			case "enroll":
				return errf(401, "enrollment_required", "Debes registrar un segundo factor antes de continuar.")
			}
			return errf(403, "forbidden", "No permitido.")
		}
		return fn(w, r.WithContext(context.WithValue(r.Context(), keyIdentity, id)))
	})
}

func (s *Server) full(fn handler) http.HandlerFunc { return s.authed([]string{"full"}, fn) }

func (s *Server) stepUpValid(id *identity) bool {
	return id.sess.StepUpAt != nil && s.clock.Now().Sub(*id.sess.StepUpAt) < stepUpTTL
}

// withStepUp exige re-autenticación reciente (passkey o TOTP en los últimos 5 min).
func (s *Server) withStepUp(fn handler) http.HandlerFunc {
	return s.full(func(w http.ResponseWriter, r *http.Request) error {
		if !s.stepUpValid(ident(r)) {
			return errStepUp
		}
		return fn(w, r)
	})
}

// enrollOrStepUp: durante el alta del 2.º factor no hace falta step-up;
// con sesión completa (agregar/reemplazar factores) sí.
func (s *Server) enrollOrStepUp(fn handler) http.HandlerFunc {
	return s.authed([]string{"enroll", "full"}, func(w http.ResponseWriter, r *http.Request) error {
		id := ident(r)
		if id.sess.State == "full" && !s.stepUpValid(id) {
			return errStepUp
		}
		return fn(w, r)
	})
}

func (s *Server) audit(r *http.Request, userID, event string, meta map[string]string) {
	s.store.Audit(r.Context(), userID, event, clientIP(r), truncate(r.UserAgent(), 200), meta)
}

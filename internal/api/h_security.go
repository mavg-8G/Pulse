package api

import (
	"net/http"
	"time"

	"pulse/internal/auth"
	"pulse/internal/buildinfo"
	"pulse/internal/store"
)

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	m, err := s.store.MFA(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{
		"id": id.user.ID, "email": id.user.Email, "role": id.user.Role, "timezone": id.user.Timezone,
		"theme": id.user.Theme, "created_at": id.user.CreatedAt, "mfa": m, "step_up_valid": s.stepUpValid(id),
		"build": buildinfo.Get(), // para comprobar tras desplegar qué versión corre en el servidor
	})
	return nil
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Theme    *string `json:"theme"`
		Timezone *string `json:"timezone"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Theme != nil && *in.Theme != "system" && *in.Theme != "light" && *in.Theme != "dark" {
		return badRequest("Tema inválido.")
	}
	if in.Timezone != nil && !s.validTZ(*in.Timezone) {
		return badRequest("Zona horaria inválida.")
	}
	if err := s.store.UpdatePrefs(r.Context(), id.user.ID, in.Theme, in.Timezone); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if ok, _ := s.hasher.Verify(id.user.PasswordHash, in.Current); !ok {
		s.audit(r, id.user.ID, "password_change_failed", nil)
		return badRequest("La contraseña actual no es correcta.")
	}
	if err := auth.CheckPolicy(in.New, id.user.Email); err != nil {
		return badRequest(err.Error())
	}
	h, err := s.hasher.Hash(in.New)
	if err != nil {
		return err
	}
	if err := s.store.SetPassword(r.Context(), id.user.ID, h); err != nil {
		return err
	}
	// Cierra todas las demás sesiones: si alguien más tenía acceso, lo pierde.
	if err := s.store.DeleteOtherSessions(r.Context(), id.user.ID, id.sess.ID); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "password_changed", nil)
	w.WriteHeader(204)
	return nil
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	ss, err := s.store.ListSessions(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	type out struct {
		ID         string    `json:"id"`
		IP         string    `json:"ip"`
		UserAgent  string    `json:"user_agent"`
		Remember   bool      `json:"remember"`
		CreatedAt  time.Time `json:"created_at"`
		LastSeenAt time.Time `json:"last_seen_at"`
		Current    bool      `json:"current"`
	}
	res := []out{}
	for _, se := range ss {
		res = append(res, out{se.ID, se.IP, se.UserAgent, se.Remember, se.CreatedAt, se.LastSeenAt, se.ID == id.sess.ID})
	}
	writeJSON(w, 200, res)
	return nil
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	sid := r.PathValue("id")
	if err := s.store.DeleteSession(r.Context(), id.user.ID, sid); err != nil {
		return err
	}
	if sid == id.sess.ID {
		s.clearCookie(w)
	}
	s.audit(r, id.user.ID, "session_revoked", map[string]string{"session": sid})
	w.WriteHeader(204)
	return nil
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) error {
	entries, err := s.store.AuditFor(r.Context(), ident(r).user.ID, 50)
	if err != nil {
		return err
	}
	writeJSON(w, 200, entries)
	return nil
}

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request) error {
	pks, err := s.store.Passkeys(r.Context(), ident(r).user.ID)
	if err != nil {
		return err
	}
	type out struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		CreatedAt  time.Time  `json:"created_at"`
		LastUsedAt *time.Time `json:"last_used_at"`
	}
	res := []out{}
	for _, p := range pks {
		res = append(res, out{p.ID, p.Name, p.CreatedAt, p.LastUsedAt})
	}
	writeJSON(w, 200, res)
	return nil
}

// Nunca se permite quedar sin segundo factor.
func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	m, err := s.store.MFA(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	if m.Passkeys <= 1 && !m.TOTP {
		return store.Conflict("No puedes quitar tu único segundo factor. Agrega otro primero.")
	}
	if err := s.store.DeletePasskey(r.Context(), id.user.ID, r.PathValue("id")); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "passkey_removed", nil)
	w.WriteHeader(204)
	return nil
}

func (s *Server) deleteTOTP(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	m, err := s.store.MFA(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	if m.Passkeys == 0 {
		return store.Conflict("No puedes quitar tu único segundo factor. Agrega una passkey primero.")
	}
	if err := s.store.DeleteTOTP(r.Context(), id.user.ID); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "totp_disabled", nil)
	w.WriteHeader(204)
	return nil
}

func (s *Server) regenRecovery(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	codes, err := s.newRecoveryCodes(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	s.audit(r, id.user.ID, "recovery_regenerated", nil)
	writeJSON(w, 200, map[string]any{"recovery_codes": codes})
	return nil
}

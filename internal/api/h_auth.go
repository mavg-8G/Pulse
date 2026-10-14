package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"pulse/internal/auth"
	"pulse/internal/passkey"
	"pulse/internal/store"
)

func normEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 254 || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return "", badRequest("Email inválido.")
	}
	return s, nil
}

func (s *Server) validTZ(tz string) bool {
	if tz == "" || len(tz) > 64 {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

func (s *Server) methods(ctx context.Context, userID string) ([]string, error) {
	m, err := s.store.MFA(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	if m.Passkeys > 0 {
		out = append(out, "passkey")
	}
	if m.TOTP {
		out = append(out, "totp")
	}
	if m.RecoveryRemaining > 0 {
		out = append(out, "recovery")
	}
	return out, nil
}

// GET /api/auth/state: nunca devuelve 401; el frontend lo usa para decidir la pantalla.
func (s *Server) authState(w http.ResponseWriter, r *http.Request) error {
	id, err := s.loadIdentity(r)
	if err != nil {
		return err
	}
	if id == nil {
		writeJSON(w, 200, map[string]any{"state": "none"})
		return nil
	}
	resp := map[string]any{"state": id.sess.State, "email": id.user.Email}
	if id.sess.State == "mfa" {
		if resp["methods"], err = s.methods(r.Context(), id.user.ID); err != nil {
			return err
		}
	}
	writeJSON(w, 200, resp)
	return nil
}

var defaultCategories = []struct{ name, color, icon string }{
	{"Comida", "#e76f51", "🍽️"}, {"Transporte", "#2a9d8f", "🚌"}, {"Hogar", "#8d6e63", "🏠"},
	{"Servicios", "#e9c46a", "💡"}, {"Salud", "#e5989b", "🩺"}, {"Ocio", "#9b5de5", "🎬"},
	{"Suscripciones", "#00b4d8", "🔁"}, {"Compras", "#f4a261", "🛍️"}, {"Educación", "#43aa8b", "📚"},
}

func (s *Server) seedUser(userID string) func(ctx context.Context, tx *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error {
		for _, c := range defaultCategories {
			if err := s.store.CreateCategoryTx(ctx, tx, userID, &store.Category{ID: auth.NewID(), Name: c.name, Color: c.color, Icon: c.icon}); err != nil {
				return err
			}
		}
		if err := s.store.CreateCategoryTx(ctx, tx, userID, &store.Category{ID: auth.NewID(), Name: "Otros", Color: "#78909c", Icon: "📦", IsSystem: true}); err != nil {
			return err
		}
		return s.store.CreateAccountTx(ctx, tx, userID, &store.Account{ID: auth.NewID(), Name: "Efectivo", Type: "cash", Currency: "USD", Color: "#43aa8b", Icon: "💵"})
	}
}

var errRegister = badRequest("No se pudo completar el registro. Revisa el código de invitación.")

func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	if !s.limRegister.Allow(clientIP(r)) {
		return errRateLimited
	}
	var in struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		InviteCode string `json:"invite_code"`
		Timezone   string `json:"timezone"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	email, err := normEmail(in.Email)
	if err != nil {
		return err
	}
	if err := auth.CheckPolicy(in.Password, email); err != nil {
		return badRequest(strings.ToUpper(err.Error()[:1]) + err.Error()[1:] + ".")
	}
	code := auth.NormalizeCode(in.InviteCode)
	if len(code) != 20 {
		return errRegister
	}
	tz := in.Timezone
	if !s.validTZ(tz) {
		tz = s.cfg.DefaultTZ
	}
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return err
	}
	uid := auth.NewID()
	u := &store.User{ID: uid, Email: email, PasswordHash: hash, DEKEnc: s.keys.NewDEK(uid), Timezone: tz, CreatedAt: s.clock.Now()}
	err = s.store.RegisterWithInvite(r.Context(), store.NewUserInput{User: u, CodeHash: s.keys.Lookup("invite", []byte(code)), Setup: s.seedUser(uid)})
	if errors.Is(err, store.ErrConflict) {
		s.audit(r, "", "register_failed", nil)
		return errRegister // mismo mensaje para invitación inválida y email existente
	}
	if err != nil {
		return err
	}
	s.audit(r, uid, "register", map[string]string{"role": u.Role})
	if _, err := s.newSession(r.Context(), w, r, uid, "enroll", false); err != nil {
		return err
	}
	writeJSON(w, 201, map[string]any{"state": "enroll"})
	return nil
}

var errLogin = errf(401, "invalid_credentials", "Email o contraseña incorrectos, o cuenta bloqueada temporalmente.")

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	if !s.limLogin.Allow(clientIP(r)) {
		return errRateLimited
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	u, err := s.store.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(in.Email)))
	if errors.Is(err, store.ErrNotFound) {
		s.hasher.DummyVerify(in.Password)
		s.audit(r, "", "login_failed", map[string]string{"reason": "unknown"})
		return errLogin
	}
	if err != nil {
		return err
	}
	now := s.clock.Now()
	if u.DisabledAt != nil {
		s.hasher.DummyVerify(in.Password)
		s.audit(r, u.ID, "login_failed", map[string]string{"reason": "disabled"})
		return errLogin
	}
	if u.LockedUntil != nil && now.Before(*u.LockedUntil) {
		s.hasher.DummyVerify(in.Password)
		s.audit(r, u.ID, "login_locked", nil)
		return errLogin
	}
	ok, rehash := s.hasher.Verify(u.PasswordHash, in.Password)
	if !ok {
		n, err := s.store.RegisterFailure(ctx, u.ID, func(n int) (time.Time, bool) { return auth.LockoutUntil(now, n) })
		if err != nil {
			return err
		}
		s.audit(r, u.ID, "login_failed", map[string]string{"reason": "password", "failures": itoa(n)})
		return errLogin
	}
	if rehash {
		if h, err := s.hasher.Hash(in.Password); err == nil {
			s.store.SetPassword(ctx, u.ID, h)
		}
	}
	methods, err := s.methods(ctx, u.ID)
	if err != nil {
		return err
	}
	state := "mfa"
	if !(contains(methods, "passkey") || contains(methods, "totp")) {
		state = "enroll" // registro incompleto o MFA reiniciado por el admin
	}
	if _, err := s.newSession(ctx, w, r, u.ID, state, in.Remember); err != nil {
		return err
	}
	s.audit(r, u.ID, "login_password_ok", nil)
	writeJSON(w, 200, map[string]any{"state": state, "methods": methods})
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	s.store.DeleteSession(r.Context(), id.user.ID, id.sess.ID)
	s.clearCookie(w)
	s.audit(r, id.user.ID, "logout", nil)
	w.WriteHeader(204)
	return nil
}

// ---------- segundo factor: utilidades ----------

// mfaFailure suma el fallo al contador de la cuenta: el bloqueo progresivo también
// protege contra fuerza bruta del código TOTP.
func (s *Server) mfaFailure(r *http.Request, id *identity, method string) error {
	now := s.clock.Now()
	n, err := s.store.RegisterFailure(r.Context(), id.user.ID, func(n int) (time.Time, bool) { return auth.LockoutUntil(now, n) })
	if err != nil {
		return err
	}
	s.audit(r, id.user.ID, "mfa_failed", map[string]string{"method": method, "failures": itoa(n)})
	return errf(401, "invalid_code", "Verificación incorrecta.")
}

func (s *Server) checkLocked(id *identity) error {
	if id.user.LockedUntil != nil && s.clock.Now().Before(*id.user.LockedUntil) {
		return errf(429, "locked", "Cuenta bloqueada temporalmente por intentos fallidos.")
	}
	return nil
}

func (s *Server) verifyTOTP(r *http.Request, id *identity, code string) error {
	if !s.limMFA.Allow("s:" + id.sess.ID) {
		return errRateLimited
	}
	if err := s.checkLocked(id); err != nil {
		return err
	}
	t, err := s.store.TOTP(r.Context(), id.user.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && t.ConfirmedAt == nil) {
		return badRequest("No tienes una app de autenticación configurada.")
	}
	if err != nil {
		return err
	}
	secret, err := id.sealer.OpenBytes("totp.secret", t.SecretEnc)
	if err != nil {
		return err
	}
	step, ok := auth.ValidateTOTP(secret, code, s.clock.Now(), t.LastStep)
	if !ok {
		return s.mfaFailure(r, id, "totp")
	}
	// Anti-replay atómico: si otra petición usó este paso primero, falla.
	if err := s.store.UseTOTPStep(r.Context(), id.user.ID, step); err != nil {
		return s.mfaFailure(r, id, "totp")
	}
	return s.store.ResetFailures(r.Context(), id.user.ID)
}

func (s *Server) newRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	codes := make([]string, 10)
	hashes := make([][]byte, 10)
	for i := range codes {
		codes[i] = auth.RecoveryCode()
		hashes[i] = s.keys.Lookup("recovery", []byte(normRecovery(codes[i])))
	}
	return codes, s.store.ReplaceRecoveryCodes(ctx, userID, hashes)
}

func normRecovery(c string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
}

// finishEnrollment: si era el alta inicial, entrega códigos de recuperación y completa la sesión.
func (s *Server) finishEnrollment(w http.ResponseWriter, r *http.Request, id *identity) error {
	if id.sess.State != "enroll" {
		writeJSON(w, 200, map[string]any{"state": "full"})
		return nil
	}
	codes, err := s.newRecoveryCodes(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	if err := s.upgrade(r.Context(), w, r, id); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "mfa_enrolled", nil)
	writeJSON(w, 200, map[string]any{"state": "full", "recovery_codes": codes})
	return nil
}

// ---------- TOTP ----------

func (s *Server) totpBegin(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	secret := auth.NewTOTPSecret()
	if err := s.store.PutChallenge(r.Context(), id.sess.ID, "totp-enroll", id.sealer.SealBytes("totp.pending", secret), s.clock.Now().Add(10*time.Minute)); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]string{"secret": auth.TOTPSecretString(secret), "uri": auth.TOTPURI("Pulse", id.user.Email, secret)})
	return nil
}

func (s *Server) totpFinish(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !s.limMFA.Allow("s:" + id.sess.ID) {
		return errRateLimited
	}
	data, err := s.store.GetChallenge(r.Context(), id.sess.ID, "totp-enroll")
	if errors.Is(err, store.ErrNotFound) {
		return badRequest("La configuración expiró. Vuelve a empezar.")
	}
	if err != nil {
		return err
	}
	secret, err := id.sealer.OpenBytes("totp.pending", data)
	if err != nil {
		return err
	}
	step, ok := auth.ValidateTOTP(secret, in.Code, s.clock.Now(), 0)
	if !ok {
		return errf(400, "invalid_code", "El código no coincide. Revisa la hora del teléfono y vuelve a intentar.")
	}
	if err := s.store.ConfirmTOTP(r.Context(), id.user.ID, id.sealer.SealBytes("totp.secret", secret), step); err != nil {
		return err
	}
	s.store.DeleteChallenge(r.Context(), id.sess.ID, "totp-enroll")
	s.audit(r, id.user.ID, "totp_enabled", nil)
	return s.finishEnrollment(w, r, id)
}

func (s *Server) mfaTOTP(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.verifyTOTP(r, id, in.Code); err != nil {
		return err
	}
	if err := s.upgrade(r.Context(), w, r, id); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "login_ok", map[string]string{"method": "totp"})
	writeJSON(w, 200, map[string]any{"state": "full"})
	return nil
}

func (s *Server) mfaRecovery(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !s.limMFA.Allow("s:" + id.sess.ID) {
		return errRateLimited
	}
	if err := s.checkLocked(id); err != nil {
		return err
	}
	if err := s.store.UseRecoveryCode(r.Context(), id.user.ID, s.keys.Lookup("recovery", []byte(normRecovery(in.Code)))); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return s.mfaFailure(r, id, "recovery")
		}
		return err
	}
	s.store.ResetFailures(r.Context(), id.user.ID)
	if err := s.upgrade(r.Context(), w, r, id); err != nil {
		return err
	}
	m, _ := s.store.MFA(r.Context(), id.user.ID)
	s.audit(r, id.user.ID, "login_ok", map[string]string{"method": "recovery"})
	writeJSON(w, 200, map[string]any{"state": "full", "recovery_remaining": m.RecoveryRemaining})
	return nil
}

func (s *Server) stepUpTOTP(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Code string `json:"code"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.verifyTOTP(r, id, in.Code); err != nil {
		return err
	}
	return s.grantStepUp(w, r, id, "totp")
}

func (s *Server) grantStepUp(w http.ResponseWriter, r *http.Request, id *identity, method string) error {
	if err := s.store.SetStepUp(r.Context(), id.sess.ID, s.clock.Now()); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "step_up", map[string]string{"method": method})
	writeJSON(w, 200, map[string]any{"ok": true, "valid_seconds": int(stepUpTTL.Seconds())})
	return nil
}

// ---------- Passkeys ----------

func (s *Server) userCredIDs(ctx context.Context, userID string) ([][]byte, []store.Passkey, error) {
	pks, err := s.store.Passkeys(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([][]byte, len(pks))
	for i, p := range pks {
		ids[i] = p.CredentialID
	}
	return ids, pks, nil
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	exclude, _, err := s.userCredIDs(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	ch := passkey.NewChallenge()
	if err := s.store.PutChallenge(r.Context(), id.sess.ID, "pk-register", ch, s.clock.Now().Add(5*time.Minute)); err != nil {
		return err
	}
	writeJSON(w, 200, s.rp.CreationOptions(ch, []byte(id.user.ID), id.user.Email, exclude))
	return nil
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	// El navegador añade campos extra (transports, clientExtensionResults…): lectura tolerante.
	var reg passkey.RegistrationResponse
	if err := json.Unmarshal(in.Credential, &reg); err != nil {
		return badRequest("Respuesta de passkey inválida.")
	}
	name, err := cleanText(in.Name, 40, "El nombre")
	if err != nil {
		return err
	}
	if name == "" {
		name = "Passkey"
	}
	ch, err := s.store.TakeChallenge(r.Context(), id.sess.ID, "pk-register")
	if errors.Is(err, store.ErrNotFound) {
		return badRequest("La solicitud expiró. Vuelve a intentar.")
	}
	if err != nil {
		return err
	}
	cred, err := s.rp.FinishRegistration(ch, &reg)
	if err != nil {
		s.audit(r, id.user.ID, "passkey_register_failed", map[string]string{"err": err.Error()})
		return badRequest("No se pudo verificar la passkey.")
	}
	if err := s.store.AddPasskey(r.Context(), id.user.ID, &store.Passkey{ID: auth.NewID(), CredentialID: cred.ID, PublicKey: cred.PublicKey, Alg: cred.Alg, SignCount: cred.SignCount, Name: name}); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "passkey_added", map[string]string{"name": name})
	return s.finishEnrollment(w, r, id)
}

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	allow, _, err := s.userCredIDs(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	if len(allow) == 0 {
		return badRequest("No tienes passkeys registradas.")
	}
	ch := passkey.NewChallenge()
	if err := s.store.PutChallenge(r.Context(), id.sess.ID, "pk-login", ch, s.clock.Now().Add(5*time.Minute)); err != nil {
		return err
	}
	writeJSON(w, 200, s.rp.RequestOptions(ch, allow))
	return nil
}

func (s *Server) verifyPasskey(r *http.Request, id *identity, raw json.RawMessage) error {
	if !s.limMFA.Allow("s:" + id.sess.ID) {
		return errRateLimited
	}
	if err := s.checkLocked(id); err != nil {
		return err
	}
	var resp passkey.AssertionResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return badRequest("Respuesta de passkey inválida.")
	}
	ch, err := s.store.TakeChallenge(r.Context(), id.sess.ID, "pk-login")
	if errors.Is(err, store.ErrNotFound) {
		return badRequest("La solicitud expiró. Vuelve a intentar.")
	}
	if err != nil {
		return err
	}
	_, pks, err := s.userCredIDs(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	lookup := func(cid []byte) (*passkey.Credential, error) {
		for _, p := range pks {
			if string(p.CredentialID) == string(cid) {
				return &passkey.Credential{ID: p.CredentialID, PublicKey: p.PublicKey, Alg: p.Alg, SignCount: p.SignCount}, nil
			}
		}
		return nil, passkey.ErrUnknown
	}
	cred, count, err := s.rp.FinishLogin(ch, &resp, lookup)
	if err != nil {
		s.log.Warn("passkey rechazada", "user", id.user.ID, "err", err.Error())
		return s.mfaFailure(r, id, "passkey")
	}
	if err := s.store.UpdatePasskeyCounter(r.Context(), id.user.ID, cred.ID, count); err != nil {
		return err
	}
	return s.store.ResetFailures(r.Context(), id.user.ID)
}

func (s *Server) mfaPasskeyFinish(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Credential json.RawMessage `json:"credential"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.verifyPasskey(r, id, in.Credential); err != nil {
		return err
	}
	if err := s.upgrade(r.Context(), w, r, id); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "login_ok", map[string]string{"method": "passkey"})
	writeJSON(w, 200, map[string]any{"state": "full"})
	return nil
}

func (s *Server) stepUpPasskey(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	var in struct {
		Credential json.RawMessage `json:"credential"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.verifyPasskey(r, id, in.Credential); err != nil {
		return err
	}
	return s.grantStepUp(w, r, id, "passkey")
}

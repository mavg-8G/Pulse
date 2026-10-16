package api_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pulse/internal/admin"
	"pulse/internal/passkey"
	"pulse/internal/passkey/passkeytest"
)

func TestRegistroConInvitacion(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	body := func(code, email string) map[string]any {
		return map[string]any{"email": email, "password": "una-clave-larga-y-segura", "invite_code": code}
	}

	t.Run("código inexistente", func(t *testing.T) {
		r := c.must(400, "POST", "/api/auth/register", body("PLS-AAAA-BBBB-CCCC-DDDD-EEEE", "a@x.com"))
		if !strings.Contains(r.Raw, "invitación") {
			t.Fatal("mensaje genérico esperado")
		}
	})
	t.Run("contraseña débil", func(t *testing.T) {
		c.must(400, "POST", "/api/auth/register", map[string]any{"email": "a@x.com", "password": "password1234", "invite_code": e.invite("")})
	})
	t.Run("invitación atada a otro email", func(t *testing.T) {
		c.must(400, "POST", "/api/auth/register", body(e.invite("otra@x.com"), "a@x.com"))
	})
	t.Run("invitación caducada", func(t *testing.T) {
		code := e.invite("")
		e.clock.Advance(73 * time.Hour)
		c.must(400, "POST", "/api/auth/register", body(code, "a@x.com"))
	})
	t.Run("invitación revocada", func(t *testing.T) {
		code, id, _ := admin.CreateInvite(context.Background(), e.st, e.keys, "", "user", time.Hour)
		if err := e.st.RevokeInvite(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		c.must(400, "POST", "/api/auth/register", body(code, "a@x.com"))
	})
	t.Run("válida, minúsculas y sin guiones también", func(t *testing.T) {
		code := strings.ToLower(strings.ReplaceAll(e.invite(""), "-", " "))
		r := c.must(201, "POST", "/api/auth/register", body(code, "Nuevo@X.com"))
		if r.str("state") != "enroll" {
			t.Fatal("tras registrarse debe registrar un 2.º factor")
		}
		// Sin 2.º factor no se accede a nada
		if r := c.do("GET", "/api/me", nil); r.Code != 401 || r.errCode() != "enrollment_required" {
			t.Fatalf("esperaba enrollment_required: %s", r.Raw)
		}
		if r := c.do("GET", "/api/transactions", nil); r.Code != 401 {
			t.Fatal("datos accesibles sin 2.º factor")
		}
		// El mismo código no se reutiliza
		c2 := e.client()
		c2.ip = "203.0.113.99"
		c2.must(400, "POST", "/api/auth/register", body(code, "otro@x.com"))
	})
	t.Run("email duplicado da el mismo error genérico", func(t *testing.T) {
		c3 := e.client()
		c3.ip = "203.0.113.50"
		r := c3.must(400, "POST", "/api/auth/register", body(e.invite(""), "nuevo@x.com"))
		if !strings.Contains(r.Raw, "invitación") {
			t.Fatal("no debe revelar que el email existe")
		}
	})
}

func TestLimiteDeRegistro(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	for i := 0; i < 5; i++ {
		c.do("POST", "/api/auth/register", map[string]any{"email": "a@x.com", "password": "x", "invite_code": "y"})
	}
	if r := c.do("POST", "/api/auth/register", map[string]any{"email": "a@x.com", "password": "x", "invite_code": "y"}); r.Code != 429 {
		t.Fatalf("el 6.º intento debe limitarse: %d", r.Code)
	}
}

func TestEnrolamientoTOTPRechazaCodigoMalo(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	c.must(201, "POST", "/api/auth/register", map[string]any{"email": "a@x.com", "password": "una-clave-larga-y-segura", "invite_code": e.invite("")})
	c.must(200, "POST", "/api/auth/enroll/totp/begin", nil)
	c.must(400, "POST", "/api/auth/enroll/totp/finish", map[string]any{"code": "000000"})
}

func TestLoginCompletoYRotacionDeToken(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	c.must(204, "POST", "/api/auth/logout", map[string]any{})
	if c.cookie != "" {
		t.Fatal("logout debe borrar la cookie")
	}

	r := c.must(200, "POST", "/api/auth/login", map[string]any{"email": "ANA@x.com", "password": c.pw})
	if r.str("state") != "mfa" {
		t.Fatalf("esperaba estado mfa: %s", r.Raw)
	}
	methods, _ := json.Marshal(r.Body["methods"])
	if !strings.Contains(string(methods), "totp") || !strings.Contains(string(methods), "recovery") {
		t.Fatalf("métodos inesperados: %s", methods)
	}
	partial := c.cookie
	if r := c.do("GET", "/api/me", nil); r.errCode() != "mfa_required" {
		t.Fatalf("sin 2.º factor debe pedir MFA: %s", r.Raw)
	}
	c.nextStep()
	c.must(200, "POST", "/api/auth/mfa/totp", map[string]any{"code": c.totpCode()})
	if c.cookie == partial || c.cookie == "" {
		t.Fatal("el token debe rotar al completar el 2.º factor")
	}
	if b, _ := c.must(200, "GET", "/api/me", nil).Body["build"].(map[string]any); b["built_at"] == "" || b["built_at"] == nil {
		t.Fatalf("/api/me debe decir qué compilación corre: %v", b)
	}

	// El token parcial anterior ya no sirve
	old := e.client()
	old.cookie = partial
	if r := old.do("GET", "/api/auth/state", nil); r.str("state") != "none" {
		t.Fatal("el token parcial debió invalidarse")
	}
}

func TestTOTPNoSeReutiliza(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	c.nextStep()
	code := c.totpCode()
	c.cookie = ""
	c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	c.must(200, "POST", "/api/auth/mfa/totp", map[string]any{"code": code})
	// Un atacante que vio el código intenta usarlo en otra sesión
	a := e.client()
	a.ip = "198.51.100.1"
	a.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	if r := a.do("POST", "/api/auth/mfa/totp", map[string]any{"code": code}); r.Code != 401 {
		t.Fatalf("código reutilizado aceptado: %d", r.Code)
	}
}

func TestCredencialesIncorrectasYBloqueo(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	c.cookie = ""
	unknown := c.must(401, "POST", "/api/auth/login", map[string]any{"email": "nadie@x.com", "password": "x"})
	wrong := c.must(401, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": "incorrecta-123"})
	if unknown.errCode() != wrong.errCode() || unknown.Raw != wrong.Raw {
		t.Fatal("email desconocido y contraseña errónea deben responder igual")
	}
	for i := 0; i < 4; i++ {
		c.must(401, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": "incorrecta-123"})
	}
	// 5 fallos → bloqueada: ni con la contraseña correcta
	c.must(401, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	e.clock.Advance(61 * time.Second)
	c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
}

func TestLimitePorIPEnLogin(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	for i := 0; i < 10; i++ {
		c.do("POST", "/api/auth/login", map[string]any{"email": "a@x.com", "password": "x"})
	}
	if r := c.do("POST", "/api/auth/login", map[string]any{"email": "a@x.com", "password": "x"}); r.Code != 429 {
		t.Fatalf("esperaba 429, obtuve %d", r.Code)
	}
}

func TestFuerzaBrutaDeTOTPBloquea(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	c.cookie = ""
	c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	for i := 0; i < 6; i++ {
		c.do("POST", "/api/auth/mfa/totp", map[string]any{"code": "000000"})
	}
	c.nextStep()
	if r := c.do("POST", "/api/auth/mfa/totp", map[string]any{"code": c.totpCode()}); r.Code == 200 {
		t.Fatal("tras 6 códigos erróneos el código correcto no debe pasar de inmediato")
	}
}

func TestCodigoDeRecuperacionUnSoloUso(t *testing.T) {
	e2 := newEnv(t)
	u := e2.client()
	u.email, u.pw = "b@x.com", "una-clave-larga-y-segura"
	u.must(201, "POST", "/api/auth/register", map[string]any{"email": u.email, "password": u.pw, "invite_code": e2.invite("")})
	begin := u.must(200, "POST", "/api/auth/enroll/totp/begin", nil)
	u.totp = mustB32(t, begin.str("secret"))
	fin := u.must(200, "POST", "/api/auth/enroll/totp/finish", map[string]any{"code": u.totpCode()})
	codes := fin.Body["recovery_codes"].([]any)
	code := codes[3].(string)

	u.cookie = ""
	u.must(200, "POST", "/api/auth/login", map[string]any{"email": u.email, "password": u.pw})
	ok := u.must(200, "POST", "/api/auth/mfa/recovery", map[string]any{"code": strings.ToUpper(code)})
	if ok.Body["recovery_remaining"].(float64) != 9 {
		t.Fatal("debe quedar 9 códigos")
	}
	u.cookie = ""
	u.must(200, "POST", "/api/auth/login", map[string]any{"email": u.email, "password": u.pw})
	u.must(401, "POST", "/api/auth/mfa/recovery", map[string]any{"code": code})
}

func TestPasskeyComoSegundoFactor(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	dev := passkeytest.New("pulse.test", origin)

	// Agregar una passkey exige step-up
	if r := c.do("POST", "/api/auth/enroll/passkey/begin", map[string]any{}); r.errCode() != "step_up_required" {
		t.Fatalf("esperaba step_up_required: %s", r.Raw)
	}
	c.stepUp()
	opts := c.must(200, "POST", "/api/auth/enroll/passkey/begin", map[string]any{})
	ch := mustB64(t, opts.str("challenge"))
	reg, _ := json.Marshal(dev.Register(ch))
	var regAny map[string]any
	json.Unmarshal(reg, &regAny)
	regAny["clientExtensionResults"] = map[string]any{} // campos extra del navegador
	c.must(200, "POST", "/api/auth/enroll/passkey/finish", map[string]any{"name": "Mi teléfono", "credential": regAny})

	pks := c.must(200, "GET", "/api/security/passkeys", nil)
	if len(pks.List) != 1 || pks.List[0].(map[string]any)["name"] != "Mi teléfono" {
		t.Fatalf("passkeys: %s", pks.Raw)
	}

	// Login con passkey
	c.cookie = ""
	c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	req := c.must(200, "POST", "/api/auth/mfa/passkey/begin", map[string]any{})
	ch = mustB64(t, req.str("challenge"))
	c.must(200, "POST", "/api/auth/mfa/passkey/finish", map[string]any{"credential": dev.Login(ch)})
	c.must(200, "GET", "/api/me", nil)

	// Un desafío no se puede usar dos veces
	c.cookie = ""
	c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	req = c.must(200, "POST", "/api/auth/mfa/passkey/begin", map[string]any{})
	ch = mustB64(t, req.str("challenge"))
	assertion := dev.Login(ch)
	c.must(200, "POST", "/api/auth/mfa/passkey/finish", map[string]any{"credential": assertion})
	c2 := e.client()
	c2.ip = "198.51.100.3"
	c2.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	c2.must(200, "POST", "/api/auth/mfa/passkey/begin", map[string]any{})
	if r := c2.do("POST", "/api/auth/mfa/passkey/finish", map[string]any{"credential": assertion}); r.Code == 200 {
		t.Fatal("una aserción capturada no debe servir en otra sesión")
	}

	// Passkey de otro dispositivo (no registrada) → rechazada
	c3 := e.client()
	c3.ip = "198.51.100.4"
	c3.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	req = c3.must(200, "POST", "/api/auth/mfa/passkey/begin", map[string]any{})
	other := passkeytest.New("pulse.test", origin)
	c3.must(401, "POST", "/api/auth/mfa/passkey/finish", map[string]any{"credential": other.Login(mustB64(t, req.str("challenge")))})
}

func TestNoSePuedeQuedarSinSegundoFactor(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	c.stepUp()
	c.must(409, "DELETE", "/api/security/totp", nil)
}

func TestExpiracionDeSesiones(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")

	t.Run("parcial caduca a los 10 min", func(t *testing.T) {
		p := e.client()
		p.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
		e.clock.Advance(11 * time.Minute)
		if r := p.do("GET", "/api/auth/state", nil); r.str("state") != "none" {
			t.Fatal("la sesión parcial debió caducar")
		}
	})
	t.Run("inactividad 12 h", func(t *testing.T) {
		c.login()
		e.clock.Advance(11 * time.Hour)
		c.must(200, "GET", "/api/me", nil) // la actividad renueva
		e.clock.Advance(11 * time.Hour)
		c.must(200, "GET", "/api/me", nil)
		e.clock.Advance(12*time.Hour + time.Minute)
		c.must(401, "GET", "/api/me", nil)
	})
	t.Run("absoluta 30 días aunque haya actividad", func(t *testing.T) {
		c.cookie = ""
		c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw, "remember": true})
		c.nextStep()
		c.must(200, "POST", "/api/auth/mfa/totp", map[string]any{"code": c.totpCode()})
		for i := 0; i < 29; i++ {
			e.clock.Advance(24 * time.Hour)
			c.must(200, "GET", "/api/me", nil)
		}
		e.clock.Advance(25 * time.Hour)
		c.must(401, "GET", "/api/me", nil)
	})
}

func TestStepUpYCambioDeContrasena(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	other := e.client()
	other.email, other.pw, other.totp = c.email, c.pw, c.totp
	other.ip = "198.51.100.9"
	other.login()

	body := map[string]any{"current_password": c.pw, "new_password": "otra-clave-muy-segura"}
	if r := c.do("POST", "/api/me/password", body); r.errCode() != "step_up_required" {
		t.Fatalf("esperaba step_up_required: %s", r.Raw)
	}
	c.stepUp()
	c.must(400, "POST", "/api/me/password", map[string]any{"current_password": "mala-mala-mala", "new_password": "otra-clave-muy-segura"})
	c.must(204, "POST", "/api/me/password", body)
	// La otra sesión se cerró
	other.must(401, "GET", "/api/me", nil)
	// El step-up caduca a los 5 min
	e.clock.Advance(6 * time.Minute)
	if r := c.do("POST", "/api/me/password", body); r.errCode() != "step_up_required" {
		t.Fatal("el step-up debió caducar")
	}
	c.pw = "otra-clave-muy-segura"
	c.login()
}

func TestUsuarioDeshabilitadoPierdeSesion(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	if _, err := e.st.SetDisabled(context.Background(), "ana@x.com", true); err != nil {
		t.Fatal(err)
	}
	c.must(401, "GET", "/api/me", nil)
	c.must(401, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
}

func TestReinicioDeMFAObligaAReenrolar(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	u, _ := e.st.UserByEmail(context.Background(), c.email)
	e.st.ResetMFA(context.Background(), u.ID)
	c.must(401, "GET", "/api/me", nil)
	r := c.must(200, "POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw})
	if r.str("state") != "enroll" {
		t.Fatal("sin factores debe volver a enrolar")
	}
}

func TestSesionesYActividad(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	other := e.client()
	other.email, other.pw, other.totp, other.ip = c.email, c.pw, c.totp, "198.51.100.20"
	other.login()

	r := c.must(200, "GET", "/api/security/sessions", nil)
	if len(r.List) != 2 {
		t.Fatalf("esperaba 2 sesiones: %s", r.Raw)
	}
	var otherID string
	for _, x := range r.List {
		m := x.(map[string]any)
		if m["current"] == false {
			otherID = m["id"].(string)
		}
	}
	c.must(204, "DELETE", "/api/security/sessions/"+otherID, nil)
	other.must(401, "GET", "/api/me", nil)

	act := c.must(200, "GET", "/api/security/activity", nil)
	events := act.Raw
	for _, ev := range []string{"register", "mfa_enrolled", "login_ok", "session_revoked"} {
		if !strings.Contains(events, `"`+ev+`"`) {
			t.Errorf("falta el evento %s en la auditoría", ev)
		}
	}
}

func mustB64(t *testing.T, s string) []byte {
	var b passkey.B64
	if err := json.Unmarshal([]byte(`"`+s+`"`), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

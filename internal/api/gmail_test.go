package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"pulse/internal/api"
	"pulse/internal/auth"
	"pulse/internal/config"
	"pulse/internal/gmail"
	"pulse/internal/gmail/gmailtest"
	"pulse/internal/push"
)

const pubsubSA = "pubsub-push@pulse-123456.iam.gserviceaccount.com"

type gmailEnv struct {
	g    *gmailtest.Server
	push *push.Service
	rec  *pushRecorder
}

// withGmail monta el servidor con Gmail (contra el Google falso), Pub/Sub y push.
func withGmail(t *testing.T, e *env, gemini bool) *gmailEnv {
	t.Helper()
	_, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(func(k string) string {
		return map[string]string{"PULSE_BASE_URL": origin, "PULSE_VAPID_PUBLIC_KEY": pub, "PULSE_VAPID_PRIVATE_FILE": "test", "PULSE_VAPID_SUBJECT": "mailto:test@example.com",
			"PULSE_GOOGLE_CLIENT_ID": gmailtest.ClientID, "PULSE_GOOGLE_CLIENT_SECRET_FILE": "test",
			"PULSE_GMAIL_PUBSUB_TOPIC": "projects/pulse-123456/topics/gmail", "PULSE_GMAIL_PUBSUB_SA": pubsubSA}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	g := gmailtest.New()
	t.Cleanup(g.Close)
	rec := &pushRecorder{}
	svc := &push.Service{Store: e.st, Keys: e.keys, Sender: rec}
	gs := &api.GoogleSettings{ClientID: gmailtest.ClientID, ClientSecret: gmailtest.ClientSecret, PubSubTopic: cfg.GmailPubSubTopic, PubSubSA: pubsubSA}
	if gemini {
		gs.GeminiKey = "k"
	}
	ep := g.Endpoints()
	srv := api.New(api.Deps{Config: cfg, Store: e.st, Keys: e.keys, Hasher: &auth.Hasher{Keys: e.keys, Iterations: 1000}, Clock: e.clock, Push: svc, Google: gs, GoogleEP: &ep,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	e.h = srv.Handler()
	return &gmailEnv{g: g, push: svc, rec: rec}
}

// connectGmail hace el flujo completo: step-up → URL de Google → "aceptar" → callback.
func (ge *gmailEnv) connectGmail(t *testing.T, c *client, email string) {
	t.Helper()
	c.stepUp()
	authURL := c.must(200, "POST", "/api/gmail/connect", nil).str("url")
	u, _ := url.Parse(authURL)
	q := u.Query()
	if q.Get("redirect_uri") != origin+"/ajustes/gmail" || q.Get("scope") != gmail.Scope || q.Get("code_challenge_method") != "S256" || q.Get("prompt") != "consent" {
		t.Fatalf("URL de autorización incorrecta: %s", authURL)
	}
	q.Set("login_hint", email)
	u.RawQuery = q.Encode()
	hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := hc.Get(u.String())
	if err != nil || res.StatusCode != 302 {
		t.Fatalf("Google falso: %v %v", res, err)
	}
	back, _ := url.Parse(res.Header.Get("Location"))
	if back.Path != "/ajustes/gmail" {
		t.Fatalf("Google debe volver a /ajustes/gmail: %s", back)
	}
	r := c.must(200, "POST", "/api/gmail/callback", map[string]any{"code": back.Query().Get("code"), "state": back.Query().Get("state")})
	if r.str("email") != email {
		t.Fatalf("email conectado: %s", r.Raw)
	}
}

func TestGmailSinConfigurar(t *testing.T) {
	e := newEnv(t)
	c := e.signup("g0@example.com")
	r := c.must(200, "GET", "/api/gmail", nil)
	if r.Body["available"] != false || r.Body["connected"] != false {
		t.Fatalf("%s", r.Raw)
	}
	c.stepUp()
	c.must(503, "POST", "/api/gmail/connect", nil)
	c.must(503, "POST", "/api/gmail/sync", nil)
	if r := c.do("POST", "/api/gmail/webhook", "{}", noOrigin); r.Code != 404 {
		t.Fatalf("webhook sin Pub/Sub: %d", r.Code)
	}
}

func TestGmailConectarSincronizarDesconectar(t *testing.T) {
	e := newEnv(t)
	ge := withGmail(t, e, false)
	a := e.signup("ga@example.com")
	b := e.signup("gb@example.com")

	a.must(201, "POST", "/api/push/subscribe", subscriptionBody(t, "https://fcm.googleapis.com/fcm/send/a"))
	e.clock.Advance(time.Second)

	// Conectar exige step-up.
	if r := a.do("POST", "/api/gmail/connect", nil); r.errCode() != "step_up_required" {
		t.Fatalf("conectar sin step-up: %s", r.Raw)
	}
	ge.connectGmail(t, a, "matias@gmail.com")
	st := a.must(200, "GET", "/api/gmail", nil)
	if st.Body["connected"] != true || st.str("email") != "matias@gmail.com" || st.Body["shadow"] != true || st.Body["realtime"] != true || st.str("status") != "active" {
		t.Fatalf("estado: %s", st.Raw)
	}
	if b.must(200, "GET", "/api/gmail", nil).Body["connected"] != false {
		t.Fatal("B no debe ver la conexión de A")
	}

	// Regla de comercio + presupuesto pequeño: el gasto importado debe disparar la alerta.
	comida := a.categoryID("Comida")
	a.must(201, "POST", "/api/merchant-rules", map[string]any{"pattern": "Carl S Alban Borja", "category_id": comida})
	a.must(201, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 1000})

	raw, _ := os.ReadFile("../mailparse/testdata/pichincha-consumo.eml")
	reciente := strings.ReplaceAll(string(raw), "2026-10-12 18:24", "2026-10-14 09:30")
	ge.g.Deliver("matias@gmail.com", []byte(reciente))
	r := a.must(200, "POST", "/api/gmail/sync", nil)
	if r.Body["created"] != float64(1) {
		t.Fatalf("sincronización: %s", r.Raw)
	}
	list := a.must(200, "GET", "/api/transactions?status=pending_review&month=2026-10", nil)
	items, _ := list.Body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("pendientes: %s", list.Raw)
	}
	tx := items[0].(map[string]any)
	if tx["source"] != "gmail" || tx["category_id"] != comida || tx["amount_cents"] != float64(1768) {
		t.Fatalf("gasto importado: %v", tx)
	}
	if alerts := a.must(200, "GET", "/api/alerts", nil); len(alerts.List) == 0 {
		t.Fatalf("el gasto por Gmail debe recalcular el presupuesto: %s", alerts.Raw)
	}
	if err := ge.push.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	titles := []string{}
	for _, p := range ge.rec.calls {
		titles = append(titles, p.Title)
		if strings.Contains(p.Body, "17.68") || strings.Contains(p.Body, "Carl") {
			t.Fatalf("con 'ocultar montos' (por defecto) el aviso no debe mostrar monto ni comercio: %q", p.Body)
		}
	}
	joined := strings.Join(titles, "|")
	if !strings.Contains(joined, "Nuevo gasto por revisar") || !strings.Contains(joined, "Aviso de presupuesto") || !strings.Contains(joined, "Seguridad de Pulse") {
		t.Fatalf("avisos push: %v", titles)
	}

	// B no puede sincronizar ni desconectar a A.
	b.must(409, "POST", "/api/gmail/sync", nil)
	b.must(409, "DELETE", "/api/gmail", nil)

	// Desconectar revoca en Google y borra la conexión; los gastos se quedan.
	a.must(204, "DELETE", "/api/gmail", nil)
	if a.must(200, "GET", "/api/gmail", nil).Body["connected"] != false || len(ge.g.Revoked) == 0 {
		t.Fatal("la desconexión no se completó")
	}
	if l := a.must(200, "GET", "/api/transactions?month=2026-10", nil); l.Body["total"] != float64(1) {
		t.Fatalf("los gastos importados deben conservarse: %s", l.Raw)
	}
	events := []string{}
	for _, x := range a.must(200, "GET", "/api/security/activity", nil).List {
		events = append(events, x.(map[string]any)["event"].(string))
	}
	if j := strings.Join(events, ","); !strings.Contains(j, "gmail_connected") || !strings.Contains(j, "gmail_disconnected") {
		t.Fatalf("auditoría: %v", events)
	}
}

func TestGmailCallbackRechazaStateAjeno(t *testing.T) {
	e := newEnv(t)
	withGmail(t, e, false)
	a := e.signup("gc1@example.com")
	b := e.signup("gc2@example.com")
	a.stepUp()
	u, _ := url.Parse(a.must(200, "POST", "/api/gmail/connect", nil).str("url"))
	state := u.Query().Get("state")
	// Otro usuario (o la misma persona en otra sesión) no puede usar ese state.
	if r := b.do("POST", "/api/gmail/callback", map[string]any{"code": "x", "state": state}); r.Code != 400 {
		t.Fatalf("state de otro usuario: %d", r.Code)
	}
	a2 := *a
	a2.login()
	if r := a2.do("POST", "/api/gmail/callback", map[string]any{"code": "x", "state": state}); r.Code != 400 {
		t.Fatalf("state de otra sesión: %d", r.Code)
	}
	// Caducado
	e.clock.Advance(11 * time.Minute)
	if r := a.do("POST", "/api/gmail/callback", map[string]any{"code": "x", "state": state}); r.Code == 200 {
		t.Fatal("state caducado aceptado")
	}
	// Código falso con state válido: Google lo rechaza y no queda nada guardado.
	a.stepUp()
	u, _ = url.Parse(a.must(200, "POST", "/api/gmail/connect", nil).str("url"))
	if r := a.do("POST", "/api/gmail/callback", map[string]any{"code": "inventado", "state": u.Query().Get("state")}); r.Code != 502 {
		t.Fatalf("código inventado: %d %s", r.Code, r.Raw)
	}
	if a.must(200, "GET", "/api/gmail", nil).Body["connected"] != false {
		t.Fatal("no debe quedar conectado")
	}
}

func TestGmailGeminiExigeStepUp(t *testing.T) {
	e := newEnv(t)
	ge := withGmail(t, e, true)
	a := e.signup("gg@example.com")
	ge.connectGmail(t, a, "gg@gmail.com")
	e.clock.Advance(6 * time.Minute) // el step-up de la conexión ya caducó
	if r := a.do("PUT", "/api/gmail/gemini", map[string]any{"enabled": true}); r.errCode() != "step_up_required" {
		t.Fatalf("activar Gemini sin step-up: %s", r.Raw)
	}
	a.stepUp()
	a.must(204, "PUT", "/api/gmail/gemini", map[string]any{"enabled": true})
	if st := a.must(200, "GET", "/api/gmail", nil); st.Body["gemini"] != true || st.Body["gemini_available"] != true {
		t.Fatalf("%s", st.Raw)
	}
	a.must(204, "PATCH", "/api/gmail", map[string]any{"shadow": false})
	if st := a.must(200, "GET", "/api/gmail", nil); st.Body["shadow"] != false {
		t.Fatalf("%s", st.Raw)
	}
	a.must(400, "PATCH", "/api/gmail", map[string]any{"otra": true})
}

func TestGmailGeminiNoDisponible(t *testing.T) {
	e := newEnv(t)
	ge := withGmail(t, e, false)
	a := e.signup("gn@example.com")
	ge.connectGmail(t, a, "gn@gmail.com")
	a.must(409, "PUT", "/api/gmail/gemini", map[string]any{"enabled": true})
}

func pubsubBody(email string, history int) string {
	data, _ := json.Marshal(map[string]any{"emailAddress": email, "historyId": history})
	b, _ := json.Marshal(map[string]any{"message": map[string]any{"data": base64.StdEncoding.EncodeToString(data), "messageId": "1"}, "subscription": "projects/pulse-123456/subscriptions/pulse"})
	return string(b)
}

func TestGmailWebhook(t *testing.T) {
	e := newEnv(t)
	ge := withGmail(t, e, false)
	a := e.signup("gw@example.com")
	ge.connectGmail(t, a, "gw@gmail.com")
	a.must(200, "POST", "/api/gmail/sync", nil)
	ctx := context.Background()
	anon := e.client()
	claims := func() map[string]any {
		now := e.clock.Now()
		return map[string]any{"iss": "https://accounts.google.com", "aud": origin + "/api/gmail/webhook", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "email": pubsubSA, "email_verified": true}
	}
	post := func(body string, authz string) int {
		opts := []reqOpt{noOrigin, withContentType("application/json")}
		if authz != "" {
			opts = append(opts, withHeader("Authorization", authz))
		}
		return anon.do("POST", "/api/gmail/webhook", body, opts...).Code
	}
	due := func() int { ids, _ := e.st.DueGmail(ctx); return len(ids) }
	if due() != 0 {
		t.Fatal("recién sincronizado no debe haber nada pendiente")
	}
	// Sin JWT, con JWT de otra audiencia o de otra cuenta: 401 y nada cambia.
	bad := claims()
	bad["aud"] = "https://otro.example"
	wrongSA := claims()
	wrongSA["email"] = "otro@evil-123456.iam.gserviceaccount.com"
	for name, authz := range map[string]string{"sin JWT": "", "audiencia": "Bearer " + ge.g.SignJWT(bad), "cuenta": "Bearer " + ge.g.SignJWT(wrongSA)} {
		if code := post(pubsubBody("gw@gmail.com", 5000), authz); code != 401 {
			t.Fatalf("%s: estado %d", name, code)
		}
	}
	if due() != 0 {
		t.Fatal("un aviso no autenticado no debe disparar nada")
	}
	// Cuenta desconocida: misma respuesta (no revela quién usa Pulse) y no hace nada.
	if code := post(pubsubBody("nadie@gmail.com", 5000), "Bearer "+ge.g.SignJWT(claims())); code != 204 || due() != 0 {
		t.Fatalf("cuenta desconocida: %d", code)
	}
	// Válido: adelanta la sincronización (sin CSRF: Pub/Sub no envía Origin).
	if code := post(pubsubBody("GW@gmail.com", 5000), "Bearer "+ge.g.SignJWT(claims())); code != 204 || due() != 1 {
		t.Fatalf("aviso válido: %d, pendientes %d", code, due())
	}
	// Mensaje malformado autenticado: 204 para que Pub/Sub no lo reintente sin fin.
	if code := post(`{"message":{"data":"no-es-base64!!"}}`, "Bearer "+ge.g.SignJWT(claims())); code != 204 {
		t.Fatalf("malformado: %d", code)
	}
	// Otras rutas siguen exigiendo el mismo origen.
	if r := anon.do("POST", "/api/gmail/sync", nil, noOrigin); r.Code != 403 {
		t.Fatalf("CSRF debe seguir activo fuera del webhook: %d", r.Code)
	}
}

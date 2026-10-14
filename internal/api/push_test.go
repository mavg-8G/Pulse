package api_test

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"pulse/internal/api"
	"pulse/internal/auth"
	"pulse/internal/config"
	"pulse/internal/push"
	"pulse/internal/store"
)

type pushRecorder struct{ calls []push.Payload }

func (p *pushRecorder) Send(_ context.Context, _ push.Subscription, b []byte) (push.Result, error) {
	var body push.Payload
	err := json.Unmarshal(b, &body)
	p.calls = append(p.calls, body)
	return push.Result{Status: 201}, err
}
func withPush(t *testing.T, e *env) (*push.Service, *pushRecorder) {
	t.Helper()
	_, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(func(k string) string {
		return map[string]string{"PULSE_BASE_URL": origin, "PULSE_VAPID_PUBLIC_KEY": pub, "PULSE_VAPID_PRIVATE_FILE": "test", "PULSE_VAPID_SUBJECT": "mailto:test@example.com"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &pushRecorder{}
	svc := &push.Service{Store: e.st, Keys: e.keys, Sender: recorder}
	e.h = api.New(api.Deps{Config: cfg, Store: e.st, Keys: e.keys, Hasher: &auth.Hasher{Keys: e.keys, Iterations: 1000}, Clock: e.clock, Push: svc}).Handler()
	return svc, recorder
}
func subscriptionBody(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"device_label": "Chrome de prueba", "subscription": map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), "auth": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, 16))}}}
}

func TestPushAPIIsolationAndLogout(t *testing.T) {
	e := newEnv(t)
	svc, rec := withPush(t, e)
	anon := e.client()
	anon.must(401, "GET", "/api/push/vapid-key", nil)
	a := e.signup("push-a@example.com")
	b := e.signup("push-b@example.com")
	body := subscriptionBody(t, "https://fcm.googleapis.com/push/test-a")
	a.must(403, "POST", "/api/push/subscribe", body, noOrigin)
	first := a.must(201, "POST", "/api/push/subscribe", body).str("id")
	second := a.must(201, "POST", "/api/push/subscribe", body).str("id")
	if first != second {
		t.Fatal("suscripción duplicada")
	}
	b.must(409, "POST", "/api/push/subscribe", body)
	b.must(404, "DELETE", "/api/push/subscriptions/"+first, nil)
	if len(b.must(200, "GET", "/api/push/subscriptions", nil).List) != 0 {
		t.Fatal("fuga de dispositivos")
	}
	var raw []byte
	if err := e.st.DB.QueryRow(`SELECT subscription_enc FROM push_subscriptions WHERE id=?`, first).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("fcm.googleapis.com")) {
		t.Fatal("suscripción en claro")
	}
	a.must(202, "POST", "/api/push/test", nil)
	a.must(429, "POST", "/api/push/test", nil)
	if err := svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("envíos: %d", len(rec.calls))
	}
	a.must(204, "POST", "/api/auth/logout", nil)
	var count int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE id=?`, first).Scan(&count)
	if count != 0 {
		t.Fatal("logout dejó suscripción")
	}
	b.must(201, "POST", "/api/push/subscribe", body)
	b.must(204, "DELETE", "/api/push/subscribe", nil)
	if len(b.must(200, "GET", "/api/push/subscriptions", nil).List) != 0 {
		t.Fatal("no se desactivó")
	}
}

func TestPushPreferencesValidationAndMFA(t *testing.T) {
	e := newEnv(t)
	withPush(t, e)
	a := e.signup("prefs-a@example.com")
	b := e.signup("prefs-b@example.com")
	defaults := a.must(200, "GET", "/api/push/preferences", nil)
	if defaults.Body["hide_amounts"] != true {
		t.Fatal("privacidad predeterminada")
	}
	p := store.DefaultNotificationPreferences()
	p.HideAmounts = false
	p.Weekly = false
	p.QuietEnabled = true
	a.must(200, "PUT", "/api/push/preferences", p)
	if b.must(200, "GET", "/api/push/preferences", nil).Body["hide_amounts"] != true {
		t.Fatal("preferencias cruzadas")
	}
	p.QuietEnd = p.QuietStart
	a.must(400, "PUT", "/api/push/preferences", p)
	p.QuietEnd = 1440
	a.must(400, "PUT", "/api/push/preferences", p)
	for _, endpoint := range []string{"https://127.0.0.1/push", "https://fcm.googleapis.com.evil.test/push", "http://fcm.googleapis.com/push", "https://user:pass@fcm.googleapis.com/push"} {
		a.must(400, "POST", "/api/push/subscribe", subscriptionBody(t, endpoint))
	}
	a.must(204, "POST", "/api/auth/logout", nil)
	a.must(200, "POST", "/api/auth/login", map[string]any{"email": a.email, "password": a.pw})
	a.must(401, "GET", "/api/push/preferences", nil)
	a.must(401, "POST", "/api/push/subscribe", subscriptionBody(t, "https://fcm.googleapis.com/push/mfa"))
}

func TestBudgetCreatesPushFromActualTransaction(t *testing.T) {
	e := newEnv(t)
	svc, rec := withPush(t, e)
	a := e.signup("budget-push@example.com")
	a.must(201, "POST", "/api/push/subscribe", subscriptionBody(t, "https://fcm.googleapis.com/push/budget"))
	cat := a.categoryID("Comida")
	account := a.firstID("/api/accounts")
	a.must(201, "POST", "/api/budgets", map[string]any{"category_id": cat, "amount_cents": 10000, "thresholds": "80"})
	a.must(201, "POST", "/api/transactions", map[string]any{"category_id": cat, "account_id": account, "amount_cents": 8000, "occurred_at": store.TS(e.clock.Now()), "merchant": "Prueba"})
	for i := 0; i < 2; i++ {
		if err := svc.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.calls) != 1 || rec.calls[0].URL != "/plan" {
		t.Fatalf("aviso del 80 %%: %+v", rec.calls)
	}
}

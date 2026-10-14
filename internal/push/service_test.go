package push

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"pulse/internal/auth"
	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/store"
)

type fakeSender struct {
	calls  []Payload
	status int
	retry  time.Duration
	err    error
}

func (f *fakeSender) Send(_ context.Context, _ Subscription, b []byte) (Result, error) {
	var p Payload
	if err := json.Unmarshal(b, &p); err != nil {
		return Result{}, err
	}
	f.calls = append(f.calls, p)
	return Result{Status: f.status, RetryAfter: f.retry}, f.err
}

type testEnv struct {
	st     *store.Store
	keys   *crypto.Keyring
	c      *clock.Fake
	sender *fakeSender
	svc    *Service
	uid    string
	sub    store.PushSubscription
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "push.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC))
	keys, _ := crypto.NewKeyring(bytes.Repeat([]byte{42}, 32))
	st := &store.Store{DB: d, Clock: c}
	f := &fakeSender{status: 201}
	e := &testEnv{st: st, keys: keys, c: c, sender: f, svc: &Service{Store: st, Keys: keys, Sender: f}, uid: auth.NewID()}
	e.addUser(t, e.uid)
	e.sub = e.addSub(t, e.uid)
	return e
}
func (e *testEnv) addUser(t *testing.T, uid string) {
	t.Helper()
	_, err := e.st.DB.Exec(`INSERT INTO users(id,email,password_hash,role,dek_enc,timezone,created_at) VALUES(?,?,'test','user',?,'America/Guayaquil',?)`, uid, uid+"@example.com", e.keys.NewDEK(uid), store.TS(e.c.Now()))
	if err != nil {
		t.Fatal(err)
	}
}
func (e *testEnv) addSub(t *testing.T, uid string) store.PushSubscription {
	t.Helper()
	ctx := context.Background()
	sid := auth.NewID()
	now := e.c.Now()
	se := &store.Session{ID: sid, UserID: uid, State: "full", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(100 * 24 * time.Hour), AbsoluteExpiresAt: now.Add(100 * 24 * time.Hour)}
	if err := e.st.CreateSession(ctx, se, []byte(sid)); err != nil {
		t.Fatal(err)
	}
	u, err := e.st.User(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	sl, err := e.keys.Sealer(uid, u.DEKEnc)
	if err != nil {
		t.Fatal(err)
	}
	sub := validSubscription(t, "https://fcm.googleapis.com/push/"+auth.NewID())
	raw, _ := json.Marshal(sub)
	p := store.PushSubscription{ID: auth.NewID(), UserID: uid, SessionID: sid, EndpointHash: e.keys.Lookup("push.endpoint", []byte(sub.Endpoint)), Enc: sl.SealBytes("push.subscription", raw), DeviceLabel: "Prueba"}
	if err := e.st.SavePushSubscription(ctx, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func validSubscription(t *testing.T, endpoint string) Subscription {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return Subscription{Endpoint: endpoint, Keys: webpush.Keys{P256dh: base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16))}}
}
func (e *testEnv) tick(t *testing.T) {
	t.Helper()
	if err := e.svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func (e *testEnv) queue(t *testing.T) {
	t.Helper()
	if err := e.svc.Test(context.Background(), e.uid); err != nil {
		t.Fatal(err)
	}
}
func (e *testEnv) prefs(t *testing.T, p store.NotificationPreferences) {
	t.Helper()
	if err := e.st.SetNotificationPrefs(context.Background(), e.uid, p); err != nil {
		t.Fatal(err)
	}
}

func TestRetryAndGone(t *testing.T) {
	for _, status := range []int{201, 404, 410, 429, 500} {
		t.Run(string(rune(status)), func(t *testing.T) {
			e := newTestEnv(t)
			e.sender.status = status
			e.sender.retry = 2 * time.Minute
			e.queue(t)
			e.tick(t)
			e.tick(t)
			if len(e.sender.calls) != 1 {
				t.Fatalf("envíos %d", len(e.sender.calls))
			}
			subs, err := e.st.PushSubscriptions(context.Background(), e.uid)
			if err != nil {
				t.Fatal(err)
			}
			if status == 404 || status == 410 {
				if len(subs) != 0 {
					t.Fatal("debe borrar la suscripción caducada")
				}
				return
			}
			if status == 429 || status == 500 {
				e.c.Advance(time.Minute)
				e.tick(t)
				if len(e.sender.calls) != 1 {
					t.Fatal("reintentó antes del Retry-After")
				}
				e.c.Advance(time.Minute)
				e.sender.status = 201
				e.tick(t)
				e.tick(t)
				if len(e.sender.calls) != 2 {
					t.Fatal("debe reintentar exactamente una vez")
				}
			}
		})
	}
}
func TestNetworkErrorBackoffAndPermanentError(t *testing.T) {
	e := newTestEnv(t)
	e.sender.err = errors.New("network")
	e.queue(t)
	e.tick(t)
	e.c.Advance(59 * time.Second)
	e.tick(t)
	if len(e.sender.calls) != 1 {
		t.Fatal("reintento prematuro")
	}
	e.c.Advance(time.Second)
	e.tick(t)
	e.sender.err = nil
	e.sender.status = 400
	e.c.Advance(2 * time.Minute)
	e.tick(t)
	e.c.Advance(time.Hour)
	e.tick(t)
	if len(e.sender.calls) != 3 {
		t.Fatal("error permanente se reintentó")
	}
}

func TestQuietHoursAndPrivacyAtDelivery(t *testing.T) {
	e := newTestEnv(t)
	p := store.DefaultNotificationPreferences()
	p.QuietEnabled = true
	p.QuietStart = 22 * 60
	p.QuietEnd = 8 * 60
	e.prefs(t, p)
	e.c.Set(time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)) // 23:00 Guayaquil
	u, _ := e.st.User(context.Background(), e.uid)
	sl, _ := e.keys.Sealer(u.ID, u.DEKEnc)
	if err := e.svc.queue(context.Background(), []store.PushSubscription{e.sub}, sl, "private-test", "budget", Payload{Title: "Presupuesto", Body: "Revisa tu plan.", PrivateBody: "Gastaste $80.00", URL: "/plan"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	p.HideAmounts = false
	e.prefs(t, p)
	e.tick(t)
	if len(e.sender.calls) != 0 {
		t.Fatal("envió en silencio")
	}
	// Cambiar privacidad mientras está en cola debe ocultar el monto al entregar.
	p.HideAmounts = true
	e.prefs(t, p)
	e.c.Set(time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC))
	e.tick(t)
	if len(e.sender.calls) != 1 || strings.Contains(e.sender.calls[0].Body, "80.00") {
		t.Fatal("filtró monto oculto")
	}
	p.HideAmounts = false
	e.prefs(t, p)
	if err := e.svc.queue(context.Background(), []store.PushSubscription{e.sub}, sl, "private-test-2", "budget", Payload{Body: "Revisa tu plan.", PrivateBody: "Gastaste $80.00"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if !strings.Contains(e.sender.calls[1].Body, "$80.00") {
		t.Fatal("no mostró el monto opt-in")
	}
	for _, tc := range []struct {
		start, end, h int
		quiet         bool
	}{{9 * 60, 17 * 60, 12, true}, {9 * 60, 17 * 60, 17, false}, {22 * 60, 8 * 60, 22, true}, {22 * 60, 8 * 60, 8, false}, {22 * 60, 8 * 60, 7, true}} {
		p.QuietStart, p.QuietEnd = tc.start, tc.end
		if got := QuietNow(p, time.Date(2026, 1, 1, tc.h, 0, 0, 0, time.UTC)); got != tc.quiet {
			t.Errorf("horario %+v: %v", tc, got)
		}
	}
}

func TestSummariesOncePerPeriodAndRestart(t *testing.T) {
	e := newTestEnv(t)
	p := store.DefaultNotificationPreferences()
	p.HideAmounts = false
	e.prefs(t, p)
	e.c.Set(time.Date(2026, 11, 1, 13, 59, 0, 0, time.UTC))
	e.tick(t)
	if len(e.sender.calls) != 0 {
		t.Fatal("resumen antes de 09:00")
	}
	e.c.Advance(time.Minute)
	e.tick(t)
	e.tick(t)
	if len(e.sender.calls) != 2 {
		t.Fatalf("domingo día 1: esperaba 2 resúmenes, hubo %d", len(e.sender.calls))
	}
	e.svc = &Service{Store: e.st, Keys: e.keys, Sender: e.sender}
	e.tick(t)
	if len(e.sender.calls) != 2 {
		t.Fatal("duplicados tras reinicio")
	}
	e.c.Advance(7 * 24 * time.Hour)
	e.tick(t)
	if len(e.sender.calls) != 3 {
		t.Fatal("falta resumen del siguiente domingo")
	}
	var encrypted []byte
	if err := e.st.DB.QueryRow(`SELECT payload_enc FROM push_deliveries LIMIT 1`).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("Gastos")) {
		t.Fatal("payload en claro")
	}
}

func TestSecurityIsolationLogoutAndDisabled(t *testing.T) {
	e := newTestEnv(t)
	other := auth.NewID()
	e.addUser(t, other)
	e.addSub(t, other)
	e.c.Advance(time.Second)
	e.st.Audit(context.Background(), e.uid, "login_ok", "", "", nil)
	e.st.Audit(context.Background(), e.uid, "step_up", "", "", nil)
	e.tick(t)
	e.tick(t)
	if len(e.sender.calls) != 1 || e.sender.calls[0].UserID != e.uid || e.sender.calls[0].URL != "/ajustes" {
		t.Fatal("avisos cruzados o duplicados")
	}
	if err := e.st.DeleteSession(context.Background(), e.uid, e.sub.SessionID); err != nil {
		t.Fatal(err)
	}
	e.st.Audit(context.Background(), e.uid, "password_changed", "", "", nil)
	e.tick(t)
	if len(e.sender.calls) != 1 {
		t.Fatal("push tras logout")
	}
	e.addSub(t, e.uid)
	e.queue(t)
	if _, err := e.st.DB.Exec(`UPDATE users SET disabled_at=? WHERE id=?`, store.TS(e.c.Now()), e.uid); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if len(e.sender.calls) != 1 {
		t.Fatal("push a usuario deshabilitado")
	}
}

func TestBudgetThresholdDedupAndPreferences(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	cat := &store.Category{ID: auth.NewID(), Name: "Comida", Color: "#123456", Icon: "X"}
	if err := e.st.CreateCategory(ctx, e.uid, cat); err != nil {
		t.Fatal(err)
	}
	b := &store.Budget{ID: auth.NewID(), CategoryID: cat.ID, AmountCents: 10000, Thresholds: "80", Active: true}
	if err := e.st.CreateBudget(ctx, e.uid, b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.InsertAlerts(ctx, b.ID, "2026-10", []int{80}, 8000); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	e.tick(t)
	if _, err := e.st.InsertAlerts(ctx, b.ID, "2026-10", []int{80}, 8500); err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if len(e.sender.calls) != 1 || e.sender.calls[0].URL != "/plan" {
		t.Fatal("umbral duplicado")
	}
	if strings.Contains(e.sender.calls[0].Body, "$80") {
		t.Fatal("montos visibles por defecto")
	}
	p := store.DefaultNotificationPreferences()
	p.Budgets = false
	p.Security = false
	p.Weekly = false
	p.Monthly = false
	e.prefs(t, p)
	e.st.Audit(ctx, e.uid, "login_ok", "", "", nil)
	e.c.Set(time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC))
	e.tick(t)
	if len(e.sender.calls) != 1 {
		t.Fatal("ignoró preferencias")
	}
}

func TestConcurrentTicksExactlyOnce(t *testing.T) {
	e := newTestEnv(t)
	e.queue(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.svc.Tick(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(e.sender.calls) != 1 {
		t.Fatal("envíos concurrentes duplicados")
	}
}

func TestExpiredSessionAndDelivery(t *testing.T) {
	e := newTestEnv(t)
	e.queue(t)
	e.c.Advance(49 * time.Hour)
	e.tick(t)
	if len(e.sender.calls) != 0 {
		t.Fatal("entregó un aviso vencido")
	}
	e.queue(t)
	_, err := e.st.DB.Exec(`UPDATE sessions SET expires_at=? WHERE id=?`, store.TS(e.c.Now()), e.sub.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	e.tick(t)
	if len(e.sender.calls) != 0 {
		t.Fatal("entregó a sesión expirada")
	}
}

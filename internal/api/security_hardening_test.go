package api_test

import (
	"context"
	"pulse/internal/store"
	"testing"
)

func TestVaultKeyRequiresFullSessionAndIsolatesUsers(t *testing.T) {
	e := newEnv(t)
	e.client().must(401, "GET", "/api/security/vault-key", nil)
	a := e.signup("vault-a@example.com")
	b := e.signup("vault-b@example.com")
	ra := a.must(200, "GET", "/api/security/vault-key", nil)
	rb := b.must(200, "GET", "/api/security/vault-key", nil)
	if len(ra.str("key")) != 43 || ra.str("key") == rb.str("key") {
		t.Fatal("claves locales inválidas")
	}
	if ra.Headers.Get("Cache-Control") != "no-store" {
		t.Fatal("clave cacheable")
	}
	if a.must(200, "GET", "/api/security/vault-key", nil).str("key") != ra.str("key") {
		t.Fatal("la clave no es estable")
	}
	a.must(204, "POST", "/api/auth/logout", nil)
	a.must(401, "GET", "/api/security/vault-key", nil)
	a.must(200, "POST", "/api/auth/login", map[string]any{"email": a.email, "password": a.pw})
	a.must(401, "GET", "/api/security/vault-key", nil) // contraseña sin MFA
}

func TestPasswordChangeRateLimit(t *testing.T) {
	e := newEnv(t)
	a := e.signup("password-limit@example.com")
	a.stepUp()
	for range 5 {
		a.must(400, "POST", "/api/me/password", map[string]any{"current_password": "incorrecta", "new_password": "Otra clave segura 123"})
	}
	a.must(429, "POST", "/api/me/password", map[string]any{"current_password": "incorrecta", "new_password": "Otra clave segura 123"})
}

func TestLogoutDoesNotPretendRevocationOnDatabaseFailure(t *testing.T) {
	e := newEnv(t)
	a := e.signup("logout-failure@example.com")
	if _, err := e.st.DB.Exec(`CREATE TRIGGER reject_session_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	a.must(500, "POST", "/api/auth/logout", nil)
	a.must(200, "GET", "/api/me", nil)
	if _, err := e.st.DB.Exec(`DROP TRIGGER reject_session_delete`); err != nil {
		t.Fatal(err)
	}
	a.must(204, "POST", "/api/auth/logout", nil)
	a.must(401, "GET", "/api/me", nil)
}

func TestSensitiveActionsFailClosedWithoutAudit(t *testing.T) {
	e := newEnv(t)
	a := e.signup("audit-gate@example.com")
	a.stepUp()
	if _, err := e.st.DB.Exec(`CREATE TRIGGER reject_test_audit BEFORE INSERT ON audit_log BEGIN SELECT RAISE(ABORT, 'unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	a.must(503, "POST", "/api/me/password", map[string]any{"current_password": a.pw, "new_password": "Una nueva contraseña 123"})
	// La operación protegida no se ejecutó: la contraseña anterior sigue funcionando.
	a.cookie = ""
	a.must(200, "POST", "/api/auth/login", map[string]any{"email": a.email, "password": a.pw})
}

func TestLegacyRowEncryptionMigrationAndTamper(t *testing.T) {
	e := newEnv(t)
	a := e.signup("migration@example.com")
	ctx := context.Background()
	u, err := e.st.UserByEmail(ctx, a.email)
	if err != nil {
		t.Fatal(err)
	}
	sl, err := e.keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		t.Fatal(err)
	}
	// Las alertas permiten probar la migración sin depender de cuentas de prueba.
	for _, id := range []string{"old-a", "old-b"} {
		if _, err := e.st.DB.Exec(`INSERT INTO template_alerts (id,user_id,parser,subject_enc,msg_hash,created_at) VALUES (?,?, 'bank', ?, ?, ?)`, id, u.ID, sl.Seal("template.subject", id), []byte(id), store.TS(e.clock.Now())); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.MigrateRowEncryption(ctx, e.keys); err != nil {
		t.Fatal(err)
	}
	if err := e.st.MigrateRowEncryption(ctx, e.keys); err != nil {
		t.Fatal("migración repetida", err)
	}
	alerts, err := e.st.TemplateAlerts(ctx, u.ID, sl, e.clock.Now())
	if err != nil || len(alerts) != 2 {
		t.Fatal(alerts, err)
	}
	if _, err := e.st.DB.Exec(`UPDATE template_alerts SET subject_enc = (SELECT subject_enc FROM template_alerts WHERE id = 'old-a') WHERE id = 'old-b'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.TemplateAlerts(ctx, u.ID, sl, e.clock.Now()); err == nil {
		t.Fatal("se aceptó ciphertext de otra fila")
	}
}

func TestLargeFinancialQueryFailsExplicitly(t *testing.T) {
	e := newEnv(t)
	a := e.signup("query-budget@example.com")
	u, err := e.st.UserByEmail(context.Background(), a.email)
	if err != nil {
		t.Fatal(err)
	}
	now := store.TS(e.clock.Now())
	_, err = e.st.DB.Exec(`WITH RECURSIVE seq(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM seq WHERE x < ?)
		INSERT INTO transactions(id,user_id,account_id,category_id,amount_cents,currency,occurred_at,created_at,updated_at)
		SELECT printf('large-%d',x),?,?,?,100,'USD',?,?,? FROM seq`, store.MaxQueryTransactions+1, u.ID, a.firstID("/api/accounts"), a.categoryID("Comida"), now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if r := a.must(422, "GET", "/api/transactions?month=2026-10", nil); r.errCode() != "query_limit" {
		t.Fatal(r.Raw)
	}
}

package db

import (
	"context"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *DBT {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &DBT{d}
}

func TestMigracionesSubenYBajan(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	if v, _ := Version(ctx, d.DB); v != len(ms) {
		t.Fatalf("versión %d, esperaba %d", v, len(ms))
	}
	// Idempotente
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	if err := MigrateDown(ctx, d.DB, 0); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT IN ('schema_migrations','sqlite_sequence')`).Scan(&n)
	if n != 0 {
		t.Fatalf("quedaron %d tablas tras revertir todo", n)
	}
	// Y vuelven a subir limpias
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
}

func TestPragmasDeSeguridad(t *testing.T) {
	d := open(t)
	checks := map[string]string{"foreign_keys": "1", "journal_mode": "wal", "secure_delete": "1"}
	for p, want := range checks {
		var got string
		if err := d.QueryRow("PRAGMA " + p).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %s, esperaba %s", p, got, want)
		}
	}
}

func TestAuditLogEsSoloInsercion(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO audit_log (event, created_at) VALUES ('x', 'now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE audit_log SET event = 'y'`); err == nil {
		t.Fatal("UPDATE sobre audit_log debería fallar")
	}
	if _, err := d.Exec(`DELETE FROM audit_log`); err == nil {
		t.Fatal("DELETE sobre audit_log debería fallar")
	}
}

func TestVersionFuturaRechazada(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO schema_migrations VALUES (999, 'futura', 'x')`)
	if err := Migrate(ctx, d.DB); err == nil {
		t.Fatal("debe negarse a correr sobre un esquema más nuevo")
	}
}

// La migración 5 reconstruye "transactions": los datos existentes deben sobrevivir.
func TestMigracion5ConservaDatos(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	if err := MigrateDown(ctx, d.DB, 4); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, role, dek_enc, timezone, created_at) VALUES ('u', 'a@b.c', 'x', 'user', x'00', 'UTC', 'x')`,
		`INSERT INTO accounts (id, user_id, name, type, created_at, updated_at) VALUES ('a', 'u', 'A', 'cash', 'x', 'x')`,
		`INSERT INTO categories (id, user_id, name, created_at, updated_at) VALUES ('c', 'u', 'C', 'x', 'x')`,
		`INSERT INTO transactions (id, user_id, account_id, category_id, amount_cents, occurred_at, merchant_enc, created_at, updated_at)
		 VALUES ('t', 'u', 'a', 'c', 1768, '2026-10-12T23:24:00Z', x'0102', 'x', 'x')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	var amount int
	var merchant []byte
	if err := d.QueryRow(`SELECT amount_cents, merchant_enc FROM transactions WHERE id = 't'`).Scan(&amount, &merchant); err != nil || amount != 1768 || len(merchant) != 2 {
		t.Fatalf("datos perdidos: %d %x %v", amount, merchant, err)
	}
	if _, err := d.Exec(`UPDATE transactions SET source = 'email' WHERE id = 't'`); err != nil {
		t.Fatal("la nueva versión debe aceptar source = 'email'")
	}
	// Las claves foráneas compuestas siguen vigentes
	if _, err := d.Exec(`INSERT INTO transactions (id, user_id, account_id, category_id, amount_cents, occurred_at, created_at, updated_at)
		VALUES ('t2', 'u', 'no-existe', 'c', 1, 'x', 'x', 'x')`); err == nil {
		t.Fatal("se perdió la clave foránea")
	}
	var fkErrs int
	d.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fkErrs)
	if fkErrs != 0 {
		t.Fatal("violaciones de claves foráneas tras migrar")
	}
	// Y bajar convierte 'email' en 'manual' sin perder la fila
	if err := MigrateDown(ctx, d.DB, 4); err != nil {
		t.Fatal(err)
	}
	var src string
	d.QueryRow(`SELECT source FROM transactions WHERE id = 't'`).Scan(&src)
	if src != "manual" {
		t.Fatalf("source tras bajar: %q", src)
	}
}

func TestMigracion10UneCuentasRepetidas(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	if err := MigrateDown(ctx, d.DB, 9); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, password_hash, role, dek_enc, timezone, created_at) VALUES ('u', 'a@b.c', 'x', 'user', x'00', 'UTC', 'x')`,
		`INSERT INTO categories (id, user_id, name, created_at, updated_at) VALUES ('c', 'u', 'C', 'x', 'x')`,
		`INSERT INTO accounts (id, user_id, name, type, institution, created_at, updated_at) VALUES
			('t1', 'u', 'Titanium (Diners Club)', 'credit_card', 'Titanium (Diners Club)', '2026-01-01', 'x'),
			('t2', 'u', 'Titanium (Diners Club)', 'credit_card', 'Titanium (Diners Club)', '2026-01-02', 'x'),
			('t3', 'u', 'Titanium (Diners Club)', 'credit_card', 'Titanium (Diners Club)', '2026-01-03', 'x'),
			('p1', 'u', 'Banco Pichincha …989', 'credit_card', 'Banco Pichincha', '2026-01-01', 'x'),
			('e1', 'u', 'Efectivo', 'cash', '', '2026-01-01', 'x'),
			('e2', 'u', 'Efectivo', 'cash', '', '2026-01-02', 'x')`,
		`UPDATE accounts SET card_suffix = '989' WHERE id = 'p1'`,
		`INSERT INTO transactions (id, user_id, account_id, category_id, amount_cents, occurred_at, created_at, updated_at) VALUES
			('x1', 'u', 't1', 'c', 1, 'x', 'x', 'x'), ('x2', 'u', 't2', 'c', 2, 'x', 'x', 'x'), ('x3', 'u', 't3', 'c', 3, 'x', 'x', 'x')`,
		`INSERT INTO statements (user_id, account_id, month, total_cents, updated_at) VALUES ('u', 't1', '2026-10', 5, 'x'), ('u', 't2', '2026-10', 6, 'x'), ('u', 't3', '2026-09', 7, 'x')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if err := Migrate(ctx, d.DB); err != nil {
		t.Fatal(err)
	}
	var accounts, onKeep, stmts, fkErrs int
	d.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&accounts)
	d.QueryRow(`SELECT COUNT(*) FROM transactions WHERE account_id = 't1'`).Scan(&onKeep)
	d.QueryRow(`SELECT COUNT(*) FROM statements WHERE account_id = 't1'`).Scan(&stmts)
	d.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fkErrs)
	// t1 conserva los 3 gastos; Pichincha y los dos "Efectivo" (sin banco) no se tocan
	if accounts != 4 || onKeep != 3 || stmts != 2 || fkErrs != 0 {
		t.Fatalf("cuentas %d, gastos en t1 %d, estados en t1 %d, FK %d", accounts, onKeep, stmts, fkErrs)
	}
}

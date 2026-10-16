package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pulse/internal/clock"
	"pulse/internal/db"
	"pulse/internal/store"
)

// La base rechaza por sí sola vínculos entre personas distintas, ciclos y más de un nivel,
// aunque el código de la aplicación se equivoque.
func TestVinculoDeCuentasProtegidoPorLaBase(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"ua", "ub"} {
		if _, err := d.Exec(`INSERT INTO users (id, email, password_hash, role, dek_enc, timezone, created_at) VALUES (?, ?, 'x', 'user', x'00', 'UTC', '2026-01-01T00:00:00Z')`, u, u+"@x.com"); err != nil {
			t.Fatal(err)
		}
	}
	st := &store.Store{DB: d, Clock: clock.NewFake(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))}
	for _, x := range []struct{ id, user string }{{"a1", "ua"}, {"a2", "ua"}, {"a3", "ua"}, {"b1", "ub"}} {
		if err := st.CreateAccount(ctx, x.user, &store.Account{ID: x.id, Name: x.id, Type: "bank"}); err != nil {
			t.Fatal(err)
		}
	}
	bad := []struct{ id, into string }{{"a1", "b1"}, {"b1", "a1"}, {"a1", "a1"}}
	for _, c := range bad {
		if _, err := d.Exec(`UPDATE accounts SET merged_into = ? WHERE id = ?`, c.into, c.id); err == nil {
			t.Fatalf("la base aceptó %s → %s", c.id, c.into)
		}
	}
	if err := st.MergeAccount(ctx, "ua", "a2", "a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE accounts SET merged_into = 'a2' WHERE id = 'a3'`); err == nil {
		t.Fatal("la base aceptó vincular a una cuenta que ya está vinculada")
	}
	if _, err := d.Exec(`UPDATE accounts SET merged_into = 'a3' WHERE id = 'a1'`); err == nil {
		t.Fatal("la base aceptó vincular una principal que tiene vinculadas")
	}
	if err := st.MergeAccount(ctx, "ub", "a3", "a1"); err != store.ErrNotFound {
		t.Fatalf("otra persona fusionó cuentas ajenas: %v", err)
	}
	if err := st.UnmergeAccount(ctx, "ua", "a2"); err != nil {
		t.Fatal(err)
	}
	if a, _ := st.Account(ctx, "ua", "a2"); a.MergedInto != "" {
		t.Fatal("no se separó")
	}
}

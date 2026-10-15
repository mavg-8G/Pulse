package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/store"
)

func TestExportarYBorrarUsuario(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "u.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	keys, _ := crypto.NewKeyring(bytes.Repeat([]byte{7}, 32))
	st := &store.Store{DB: d, Clock: clock.NewFake(time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC))}
	a := &app{store: st, keys: keys}

	mk := func(id, email string) *store.Transaction {
		if _, err := d.Exec(`INSERT INTO users(id,email,password_hash,role,dek_enc,timezone,created_at) VALUES(?,?,'x','user',?,'America/Guayaquil','2026-10-01T00:00:00Z')`, id, email, keys.NewDEK(id)); err != nil {
			t.Fatal(err)
		}
		u, err := st.User(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		sl, err := keys.Sealer(id, u.DEKEnc)
		if err != nil {
			t.Fatal(err)
		}
		acc := &store.Account{ID: id + "-acc", Name: "Pichincha", Type: "credit_card", Currency: "USD", Color: "#000"}
		cat := &store.Category{ID: id + "-cat", Name: "Comida", Color: "#000"}
		if err := st.CreateAccount(ctx, id, acc); err != nil {
			t.Fatal(err)
		}
		if err := st.CreateCategory(ctx, id, cat); err != nil {
			t.Fatal(err)
		}
		tx := &store.Transaction{ID: id + "-tx", AccountID: acc.ID, CategoryID: cat.ID, AmountCents: 1250, Currency: "USD", OccurredAt: time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC), Kind: "purchase", Merchant: "SUPERMAXI " + id, Source: "manual", Status: "confirmed"}
		if err := st.CreateTransaction(ctx, id, sl, tx); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertRule(ctx, id, id+"-r", sl.Seal("rule.pattern", "supermaxi"), []byte(id+"h"), cat.ID); err != nil {
			t.Fatal(err)
		}
		return tx
	}
	mk("u1", "ana@example.com")
	mk("u2", "beto@example.com")

	out := filepath.Join(t.TempDir(), "ana.json")
	if err := exportUser(ctx, a, "ana@example.com", out); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)
	if st, _ := os.Stat(out); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("permisos del export: %v", st.Mode().Perm())
	}
	var x userExport
	if err := json.Unmarshal(raw, &x); err != nil {
		t.Fatal(err)
	}
	if x.Profile.Email != "ana@example.com" || len(x.Transactions) != 1 || x.Transactions[0].Merchant != "SUPERMAXI u1" ||
		len(x.Rules) != 1 || x.Rules[0].Pattern != "supermaxi" || len(x.Accounts) != 1 {
		t.Fatalf("export incompleto o sin descifrar: %+v", x)
	}
	if strings.Contains(string(raw), "u2") || strings.Contains(string(raw), "beto") {
		t.Fatal("el export incluye datos de otra persona")
	}
	if err := exportUser(ctx, a, "ana@example.com", out); err == nil {
		t.Fatal("el export no debe sobrescribir un archivo existente")
	}

	if err := deleteUser(ctx, a, "ana@example.com", "otra@example.com"); err == nil {
		t.Fatal("borró sin la confirmación correcta")
	}
	if err := deleteUser(ctx, a, "ana@example.com", "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"users", "accounts", "categories", "transactions", "merchant_rules"} {
		var n int
		d.QueryRow(`SELECT COUNT(*) FROM ` + tbl + ` WHERE ` + map[bool]string{true: "id", false: "user_id"}[tbl == "users"] + ` = 'u1'`).Scan(&n)
		if n != 0 {
			t.Errorf("%s: quedan %d filas de la persona borrada", tbl, n)
		}
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM transactions WHERE user_id = 'u2'`).Scan(&n)
	if n != 1 {
		t.Fatalf("se borraron datos de otra persona (%d)", n)
	}
}

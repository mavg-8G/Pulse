package api_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"pulse/internal/store"
)

func TestReportesYExportacionCSV(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	acc := a.firstID("/api/accounts")
	comida := a.categoryID("Comida")
	a.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": comida, "amount_cents": 1500, "occurred_at": "2026-10-10T12:00", "merchant": "=HYPERLINK(\"x\")"})
	a.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": comida, "amount_cents": 2500, "occurred_at": "2026-09-05T12:00", "merchant": "Supermaxi"})

	r := a.must(200, "GET", "/api/reports/summary", nil)
	if r.Body["total_cents"].(float64) != 4000 || len(r.Body["months"].([]any)) != 12 || r.str("to") != "2026-10" {
		t.Fatalf("resumen: %s", r.Raw)
	}
	if r := a.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10", nil); r.Body["total_cents"].(float64) != 1500 {
		t.Fatalf("resumen de un mes: %s", r.Raw)
	}
	a.must(400, "GET", "/api/reports/summary?from=2026-11&to=2026-10", nil)
	if r := b.must(200, "GET", "/api/reports/summary", nil); r.Body["total_cents"].(float64) != 0 {
		t.Fatal("B ve los gastos de A en el resumen")
	}

	// Exportar exige step-up y neutraliza fórmulas
	if r := a.do("GET", "/api/reports/export.csv", nil); r.errCode() != "step_up_required" {
		t.Fatalf("export sin step-up: %d %s", r.Code, r.Raw)
	}
	a.stepUp()
	r = a.must(200, "GET", "/api/reports/export.csv", nil)
	if !strings.HasPrefix(r.Headers.Get("Content-Type"), "text/csv") || !strings.Contains(r.Raw, "Supermaxi") || !strings.Contains(r.Raw, `"'=HYPERLINK(""x"")"`) {
		t.Fatalf("CSV: %s", r.Raw)
	}
	b.stepUp()
	if r := b.must(200, "GET", "/api/reports/export.csv", nil); strings.Contains(r.Raw, "Supermaxi") {
		t.Fatal("B exporta gastos de A")
	}
}

func TestConciliacion(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	acc := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Visa", "type": "credit_card", "card_suffix": "989"}).str("id")
	a.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": a.categoryID("Comida"), "amount_cents": 4000, "occurred_at": "2026-10-10T12:00"})
	a.must(204, "PUT", "/api/statements", map[string]any{"account_id": acc, "month": "2026-10", "total_cents": 5250})

	r := a.must(200, "GET", "/api/reports/reconciliation?month=2026-10", nil)
	var row map[string]any
	for _, x := range r.Body["accounts"].([]any) {
		if m := x.(map[string]any); m["account_id"] == acc {
			row = m
		}
	}
	if row == nil || row["recorded_cents"].(float64) != 4000 || row["statement_cents"].(float64) != 5250 || row["difference_cents"].(float64) != 1250 {
		t.Fatalf("conciliación: %s", r.Raw)
	}
	// B no puede escribir en la cuenta de A
	b.must(400, "PUT", "/api/statements", map[string]any{"account_id": acc, "month": "2026-10", "total_cents": 1})
	a.must(204, "PUT", "/api/statements", map[string]any{"account_id": acc, "month": "2026-10", "total_cents": nil})
	if r := a.must(200, "GET", "/api/reports/reconciliation?month=2026-10", nil); strings.Contains(r.Raw, `"statement_cents":5250`) {
		t.Fatal("el total borrado sigue")
	}
}

func TestGastosRecurrentes(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	acc := a.firstID("/api/accounts")
	cat := a.categoryID("Servicios")
	// Empezó el 31 de agosto: hoy (14 oct) ya vencieron 31 ago y 30 sep; el próximo es el 31 oct.
	rec := a.must(201, "POST", "/api/recurring", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": 1999, "merchant": "Netflix", "start_date": "2026-08-31"})
	if rec.str("next_date") != "2026-10-31" {
		t.Fatalf("próxima fecha: %s", rec.Raw)
	}
	if r := a.must(200, "GET", "/api/transactions?category_id="+cat, nil); r.Body["total"].(float64) != 2 {
		t.Fatalf("esperaba 2 cobros: %s", r.Raw)
	}
	// Llegó el 31: se registra uno más y la siguiente fecha es el 30 de noviembre.
	e.clock.Advance(18 * 24 * time.Hour)
	a.login()
	b.login()
	a.must(201, "POST", "/api/recurring", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": 500, "merchant": "Spotify", "start_date": "2026-12-01"})
	list := a.must(200, "GET", "/api/recurring", nil).List
	if len(list) != 2 || list[1].(map[string]any)["next_date"] != "2026-11-30" {
		t.Fatalf("recurrentes: %v", list)
	}
	if r := a.must(200, "GET", "/api/transactions?category_id="+cat, nil); r.Body["total"].(float64) != 3 {
		t.Fatalf("esperaba 3 cobros: %s", r.Raw)
	}
	if r := b.must(200, "GET", "/api/recurring", nil); len(r.List) != 0 {
		t.Fatal("B ve los recurrentes de A")
	}
	b.must(404, "DELETE", "/api/recurring/"+rec.str("id"), nil)
	a.must(204, "DELETE", "/api/recurring/"+rec.str("id"), nil)
	a.must(400, "POST", "/api/recurring", map[string]any{"account_id": b.firstID("/api/accounts"), "category_id": cat, "amount_cents": 1, "start_date": "2026-11-01"})
}

func TestGastoOfflineNoSeDuplica(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	id := "0192f3a4-5b6c-7d8e-9f01-23456789abcd"
	body := map[string]any{"id": id, "account_id": a.firstID("/api/accounts"), "category_id": a.categoryID("Comida"), "amount_cents": 800, "occurred_at": "2026-10-14T09:00"}
	a.must(201, "POST", "/api/transactions", body)
	if r := a.must(200, "POST", "/api/transactions", body); r.str("id") != id {
		t.Fatalf("reenvío: %s", r.Raw)
	}
	if r := a.must(200, "GET", "/api/transactions", nil); r.Body["total"].(float64) != 1 {
		t.Fatalf("se duplicó: %s", r.Raw)
	}
	body["account_id"], body["category_id"] = b.firstID("/api/accounts"), b.categoryID("Comida")
	b.must(409, "POST", "/api/transactions", body) // el id de otro usuario no se pisa ni se revela
	body["id"] = "no-es-uuid"
	b.must(400, "POST", "/api/transactions", body)
}

func TestPrecisionDelModoSombra(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	u, err := e.st.UserByEmail(context.Background(), "ana@x.com")
	if err != nil {
		t.Fatal(err)
	}
	sl, _ := e.keys.Sealer(u.ID, u.DEKEnc)
	acc, cat := a.firstID("/api/accounts"), a.categoryID("Comida")
	imported := func(id string) {
		conf := 0.95
		err := e.st.CreateTransaction(context.Background(), u.ID, sl, &store.Transaction{ID: id, AccountID: acc, CategoryID: cat, AmountCents: 1000,
			Currency: "USD", OccurredAt: t0, Kind: "purchase", Source: "gmail", Status: "pending_review", Parser: "pichincha-consumo-v1", Confidence: &conf,
			MessageHash: []byte(id)})
		if err != nil {
			t.Fatal(err)
		}
	}
	imported("0192f3a4-0000-7000-8000-000000000001")
	imported("0192f3a4-0000-7000-8000-000000000002")
	a.must(200, "POST", "/api/transactions/0192f3a4-0000-7000-8000-000000000001/confirm", map[string]any{})
	// Corregir la categoría no cuenta como error del parser; el monto sí.
	a.must(200, "PATCH", "/api/transactions/0192f3a4-0000-7000-8000-000000000002", map[string]any{"amount_cents": 1100, "status": "confirmed"})
	// Volver a confirmar no cuenta dos veces
	a.must(200, "POST", "/api/transactions/0192f3a4-0000-7000-8000-000000000001/confirm", map[string]any{})

	acc0 := a.must(200, "GET", "/api/gmail", nil).Body["accuracy"].([]any)
	if len(acc0) != 1 {
		t.Fatalf("precisión: %v", acc0)
	}
	m := acc0[0].(map[string]any)
	if m["bank"] != "Banco Pichincha" || m["reviewed"].(float64) != 2 || m["correct"].(float64) != 1 || m["trusted"] != false {
		t.Fatalf("precisión: %v", m)
	}
}

func TestBancoDeConfianza(t *testing.T) {
	if (store.ParserAccuracy{Reviewed: 19, Correct: 19}).Trusted() {
		t.Error("19 revisiones no bastan")
	}
	if !(store.ParserAccuracy{Reviewed: 50, Correct: 49}).Trusted() {
		t.Error("98 % con 50 revisiones debería bastar")
	}
	if (store.ParserAccuracy{Reviewed: 50, Correct: 48}).Trusted() {
		t.Error("96 % no basta")
	}
}

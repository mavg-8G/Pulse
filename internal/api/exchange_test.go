package api_test

import (
	"context"
	"strings"
	"testing"
)

func TestExchangeSettingsArePrivate(t *testing.T) {
	e := newEnv(t)
	a := e.signup("exchange-a@example.com")
	b := e.signup("exchange-b@example.com")
	key := "secret-key-12345"
	a.stepUp()
	a.must(204, "PUT", "/api/exchange-settings", map[string]any{"api_key": key})
	if r := a.must(200, "GET", "/api/exchange-settings", nil); r.Body["configured"] != true || strings.Contains(r.Raw, key) {
		t.Fatalf("status: %s", r.Raw)
	}
	if r := b.must(200, "GET", "/api/exchange-settings", nil); r.Body["configured"] != false {
		t.Fatalf("aislamiento: %s", r.Raw)
	}
	u, err := e.st.UserByEmail(context.Background(), "exchange-a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	enc, err := e.st.ExchangeKey(context.Background(), u.ID)
	if err != nil || len(enc) == 0 || strings.Contains(string(enc), key) {
		t.Fatal("la clave debe estar cifrada")
	}
	a.must(204, "DELETE", "/api/exchange-settings", nil)
	if r := a.must(200, "GET", "/api/exchange-settings", nil); r.Body["configured"] != false {
		t.Fatal(r.Raw)
	}
}

func TestCurrencyTotalsStaySeparate(t *testing.T) {
	e := newEnv(t)
	c := e.signup("currencies@example.com")
	usd := c.firstID("/api/accounts")
	eur := c.must(201, "POST", "/api/accounts", map[string]any{"name": "Euro", "type": "credit_card", "currency": "EUR"}).str("id")
	cat := c.categoryID("Comida")
	c.must(201, "POST", "/api/transactions", map[string]any{"account_id": usd, "category_id": cat, "amount_cents": 1000, "occurred_at": "2026-10-12"})
	euroTx := c.must(201, "POST", "/api/transactions", map[string]any{"account_id": eur, "category_id": cat, "amount_cents": 2000, "occurred_at": "2026-10-12"})
	if euroTx.str("currency") != "EUR" {
		t.Fatal(euroTx.Raw)
	}
	c.must(400, "PATCH", "/api/transactions/"+euroTx.str("id"), map[string]any{"account_id": usd})
	c.must(400, "POST", "/api/budgets", map[string]any{"account_id": eur, "amount_cents": 1000})
	list := c.must(200, "GET", "/api/transactions?month=2026-10", nil)
	sums := list.Body["sums_by_currency"].(map[string]any)
	if list.Body["sum_cents"] != float64(1000) || sums["USD"] != float64(1000) || sums["EUR"] != float64(2000) {
		t.Fatalf("totales: %s", list.Raw)
	}
	dash := c.must(200, "GET", "/api/dashboard?month=2026-10", nil)
	if dash.Body["spent_cents"] != float64(1000) {
		t.Fatalf("dashboard: %s", dash.Raw)
	}
	if dash.Body["other_currencies"].(map[string]any)["EUR"] != float64(2000) {
		t.Fatalf("monedas separadas: %s", dash.Raw)
	}
	report := c.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10", nil)
	if report.Body["total_cents"] != float64(1000) || report.Body["other_currencies"].(map[string]any)["EUR"] != float64(2000) {
		t.Fatalf("reporte: %s", report.Raw)
	}
}

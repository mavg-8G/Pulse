package api_test

import (
	"fmt"
	"testing"
)

func TestDesgloseDelPlanPorCategoria(t *testing.T) {
	e := newEnv(t) // hoy: 14 oct 2026
	a := e.signup("ana@example.com")
	b := e.signup("beto@example.com")
	acc := a.firstID("/api/accounts")
	comida, hogar := a.categoryID("Comida"), a.categoryID("Hogar")
	delivery := a.must(201, "POST", "/api/categories", map[string]any{"name": "Delivery", "parent_id": comida}).str("id")
	tx := func(cat string, cents int, merchant, when, kind string) {
		a.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": cents, "merchant": merchant, "occurred_at": when, "kind": kind})
	}
	tx(comida, 3000, "Supermaxi", "2026-10-02T12:00", "purchase")
	tx(comida, 2000, "  supermaxi ", "2026-10-03T12:00", "purchase") // mismo comercio aunque cambien espacios y mayúsculas
	tx(comida, 500, "Supermaxi", "2026-10-04T12:00", "refund")
	tx(delivery, 1500, "Rappi", "2026-10-05T20:00", "purchase") // subcategoría: suma a Comida
	for i := 0; i < 7; i++ {
		tx(hogar, 100*(i+1), fmt.Sprintf("Ferretería %d", i), "2026-10-06T10:00", "purchase")
	}
	tx(comida, 9900, "Supermaxi", "2026-09-30T12:00", "purchase") // otro mes

	r := a.must(200, "GET", "/api/budgets/breakdown?month=2026-10", nil)
	cats := r.Body["categories"].([]any)
	if len(cats) != 2 {
		t.Fatalf("solo categorías con movimientos: %s", r.Raw)
	}
	c := cats[0].(map[string]any)
	if c["id"] != comida || fnum(c["spent_cents"]) != 6000 || fnum(c["purchase_count"]) != 3 || fnum(c["refund_cents"]) != 500 {
		t.Fatalf("comida: %v", c)
	}
	sub := c["subcategories"].([]any)
	if len(sub) != 1 || sub[0].(map[string]any)["id"] != delivery || fnum(sub[0].(map[string]any)["spent_cents"]) != 1500 {
		t.Fatalf("subcategorías: %v", sub)
	}
	ms := c["merchants"].([]any)
	top := ms[0].(map[string]any)
	if len(ms) != 2 || top["name"] != "Supermaxi" || fnum(top["count"]) != 3 || fnum(top["spent_cents"]) != 4500 {
		t.Fatalf("comercios: %v", ms)
	}
	h := cats[1].(map[string]any)
	if fnum(h["spent_cents"]) != 2800 || len(h["merchants"].([]any)) != 6 || fnum(h["other_count"]) != 1 || fnum(h["other_cents"]) != 100 {
		t.Fatalf("hogar: más de 6 comercios se agrupan en otros: %v", h)
	}
	if len(b.must(200, "GET", "/api/budgets/breakdown?month=2026-10", nil).Body["categories"].([]any)) != 0 {
		t.Fatal("otra persona no ve el desglose")
	}
	a.must(400, "GET", "/api/budgets/breakdown?month=2026-13", nil)
	e.client().must(401, "GET", "/api/budgets/breakdown", nil)
}

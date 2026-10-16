package api_test

import (
	"strings"
	"testing"
)

// Dos cuentas que son la misma tarjeta (una con dígitos y otra sin) se fusionan sin borrar ninguna:
// los gastos siguen en su cuenta, pero filtros, reportes, presupuestos y conciliación las suman.
func TestFusionLogicaDeCuentas(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	comida := a.categoryID("Comida")
	main := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Titanium (Diners Club)", "type": "credit_card", "institution": "Diners Club"}).str("id")
	alias := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Titanium (Diners Club) …666", "type": "credit_card", "institution": "Diners Club", "card_suffix": "666"}).str("id")
	other := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Visa", "type": "credit_card", "card_suffix": "989"}).str("id")
	txMain := a.must(201, "POST", "/api/transactions", map[string]any{"account_id": main, "category_id": comida, "amount_cents": 1000, "occurred_at": "2026-10-10T12:00"}).str("id")
	txAlias := a.must(201, "POST", "/api/transactions", map[string]any{"account_id": alias, "category_id": comida, "amount_cents": 2500, "occurred_at": "2026-10-11T12:00"}).str("id")
	a.must(201, "POST", "/api/transactions", map[string]any{"account_id": other, "category_id": comida, "amount_cents": 700, "occurred_at": "2026-10-11T12:00"})
	bud := a.must(201, "POST", "/api/budgets", map[string]any{"account_id": main, "amount_cents": 10000}).str("id")
	a.must(204, "PUT", "/api/statements", map[string]any{"account_id": alias, "month": "2026-10", "total_cents": 300})
	a.must(204, "PUT", "/api/statements", map[string]any{"account_id": main, "month": "2026-10", "total_cents": 4000})

	// Otra persona no puede fusionar sus cuentas con las de A ni las de A entre sí.
	accB := b.firstID("/api/accounts")
	b.must(404, "POST", "/api/accounts/"+accB+"/merge", map[string]any{"into": main})
	b.must(404, "POST", "/api/accounts/"+alias+"/merge", map[string]any{"into": main})
	b.must(404, "DELETE", "/api/accounts/"+alias+"/merge", nil)
	a.must(404, "POST", "/api/accounts/"+alias+"/merge", map[string]any{"into": accB})

	a.must(409, "POST", "/api/accounts/"+main+"/merge", map[string]any{"into": main})
	a.must(400, "POST", "/api/accounts/"+main+"/merge", map[string]any{})
	a.must(409, "DELETE", "/api/accounts/"+alias+"/merge", nil) // aún no está fusionada
	if r := a.must(200, "POST", "/api/accounts/"+alias+"/merge", map[string]any{"into": main}); r.str("merged_into") != main {
		t.Fatalf("fusión: %s", r.Raw)
	}
	// Un solo nivel: no se puede fusionar con una cuenta que ya está vinculada a otra.
	a.must(409, "POST", "/api/accounts/"+other+"/merge", map[string]any{"into": alias})

	// Ninguna cuenta se borró y cada gasto sigue en la suya (la lectura de correos las sigue usando).
	if r := a.must(200, "GET", "/api/accounts", nil); len(r.List) != 4 {
		t.Fatalf("cuentas: %s", r.Raw)
	}
	if r := a.must(200, "GET", "/api/transactions/"+txAlias, nil); r.str("account_id") != alias {
		t.Fatalf("el gasto cambió de cuenta: %s", r.Raw)
	}

	// Filtrar por cualquiera de las dos muestra los gastos de ambas.
	for _, id := range []string{main, alias} {
		r := a.must(200, "GET", "/api/transactions?month=2026-10&account_id="+id, nil)
		if r.Body["total"].(float64) != 2 || !strings.Contains(r.Raw, txMain) || !strings.Contains(r.Raw, txAlias) {
			t.Fatalf("filtro por %s: %s", id, r.Raw)
		}
	}

	// Reportes: una sola fila por la cuenta principal.
	r := a.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10", nil)
	byAcc := map[string]float64{}
	for _, x := range r.Body["by_account"].([]any) {
		m := x.(map[string]any)
		byAcc[m["id"].(string)] = m["spent_cents"].(float64)
	}
	if byAcc[main] != 3500 || byAcc[other] != 700 || len(byAcc) != 2 {
		t.Fatalf("reporte por cuenta: %v", byAcc)
	}

	// Presupuesto de la cuenta: cuenta los gastos de ambas.
	r = a.must(200, "GET", "/api/budgets/summary?month=2026-10", nil)
	var spent float64 = -1
	for _, x := range r.Body["lines"].([]any) {
		if m := x.(map[string]any); m["budget_id"] == bud {
			spent = m["spent_cents"].(float64)
		}
	}
	if spent != 3500 {
		t.Fatalf("presupuesto de cuenta fusionada: %v %s", spent, r.Raw)
	}

	// Conciliación: una fila con lo registrado y el estado de ambas.
	r = a.must(200, "GET", "/api/reports/reconciliation?month=2026-10", nil)
	rows := map[string]map[string]any{}
	for _, x := range r.Body["accounts"].([]any) {
		m := x.(map[string]any)
		rows[m["account_id"].(string)] = m
	}
	if _, ok := rows[alias]; ok || rows[main]["recorded_cents"].(float64) != 3500 || rows[main]["statement_cents"].(float64) != 4300 {
		t.Fatalf("conciliación: %s", r.Raw)
	}
	a.must(400, "PUT", "/api/statements", map[string]any{"account_id": alias, "month": "2026-10", "total_cents": 1})
	a.must(400, "POST", "/api/budgets", map[string]any{"account_id": alias, "amount_cents": 100})

	// Separar devuelve cada cuenta a sus propios gastos.
	if r := a.must(200, "DELETE", "/api/accounts/"+alias+"/merge", nil); r.str("merged_into") != "" {
		t.Fatalf("separar: %s", r.Raw)
	}
	if r := a.must(200, "GET", "/api/transactions?month=2026-10&account_id="+main, nil); r.Body["total"].(float64) != 1 {
		t.Fatalf("después de separar: %s", r.Raw)
	}
	r = a.must(200, "GET", "/api/budgets/summary?month=2026-10", nil)
	if !strings.Contains(r.Raw, `"spent_cents":1000`) {
		t.Fatalf("presupuesto después de separar: %s", r.Raw)
	}
}

// Fusionar una cuenta que ya tenía vinculadas las lleva todas a la nueva principal.
func TestFusionarGrupoConOtraCuenta(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	x := a.must(201, "POST", "/api/accounts", map[string]any{"name": "X", "type": "bank"}).str("id")
	y := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Y", "type": "bank"}).str("id")
	z := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Z", "type": "bank"}).str("id")
	a.must(200, "POST", "/api/accounts/"+y+"/merge", map[string]any{"into": x})
	a.must(200, "POST", "/api/accounts/"+x+"/merge", map[string]any{"into": z})
	r := a.must(200, "GET", "/api/accounts", nil)
	for _, it := range r.List {
		m := it.(map[string]any)
		want := z
		if m["id"] == z {
			want = ""
		} else if m["id"] != x && m["id"] != y {
			continue
		}
		if m["merged_into"] != want {
			t.Fatalf("%v quedó vinculada a %v, esperaba %q", m["name"], m["merged_into"], want)
		}
	}
	// Borrar la principal (sin gastos) deja sueltas a las vinculadas.
	a.must(204, "DELETE", "/api/accounts/"+z, nil)
	if acc := a.must(200, "GET", "/api/accounts", nil); strings.Contains(acc.Raw, z) {
		t.Fatalf("siguen vinculadas a una cuenta borrada: %s", acc.Raw)
	}
}

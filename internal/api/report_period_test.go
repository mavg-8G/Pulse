package api_test

import (
	"strings"
	"testing"
	"time"
)

// Periodo anterior equivalente, filtros por categoría y cuenta (con cuentas fusionadas), comercios y exportación
// filtrada. Hoy es 14 de octubre de 2026.
func TestReportesComparacionYFiltros(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	visa := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Visa", "type": "credit_card", "card_suffix": "111"}).str("id")
	vieja := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Visa vieja", "type": "credit_card", "card_suffix": "222"}).str("id")
	efectivo := a.firstID("/api/accounts")
	comida, ocio := a.categoryID("Comida"), a.categoryID("Ocio")
	delivery := a.must(201, "POST", "/api/categories", map[string]any{"name": "Delivery", "parent_id": comida}).str("id")
	tx := func(acc, cat string, cents int, when, kind, merchant string) {
		a.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": cents, "occurred_at": when, "kind": kind, "merchant": merchant})
	}
	tx(efectivo, comida, 5000, "2026-08-20T12:00", "purchase", "Primer gasto") // agosto: el periodo de septiembre es comparable
	// Septiembre (periodo anterior hasta el día 14) y lo que pasa después del 14, que no cuenta en la comparación.
	tx(visa, comida, 10000, "2026-09-03T12:00", "purchase", "Supermaxi")
	tx(efectivo, ocio, 4000, "2026-09-10T12:00", "purchase", "Cine")
	tx(visa, comida, 99900, "2026-09-25T12:00", "purchase", "Supermaxi")
	// Octubre hasta hoy.
	tx(visa, comida, 15000, "2026-10-02T12:00", "purchase", "Supermaxi")
	tx(vieja, delivery, 3000, "2026-10-05T12:00", "purchase", "supermaxi ") // misma tienda escrita distinto, cuenta que se fusionará
	tx(visa, comida, 2000, "2026-10-06T12:00", "refund", "Supermaxi")
	tx(efectivo, ocio, 1000, "2026-10-08T12:00", "purchase", "Cine")
	a.must(200, "POST", "/api/accounts/"+vieja+"/merge", map[string]any{"into": visa})

	r := a.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10", nil)
	period := r.Body["period"].(map[string]any)
	prev := r.Body["previous"].(map[string]any)
	if period["from"] != "2026-10-01" || period["to"] != "2026-10-14" || fnum(period["days"]) != 14 || period["granularity"] != "day" || period["compare"] != "previous" {
		t.Fatalf("periodo: %v", period)
	}
	// Octubre 1–14: 15 000 + 3 000 − 2 000 + 1 000 = 17 000. Septiembre 1–14: 14 000.
	if fnum(r.Body["total_cents"]) != 17000 || fnum(r.Body["purchase_cents"]) != 19000 || fnum(r.Body["refund_cents"]) != 2000 {
		t.Fatalf("totales: %s", r.Raw)
	}
	if prev["from"] != "2026-09-01" || prev["to"] != "2026-09-14" || fnum(prev["total_cents"]) != 14000 || fnum(prev["purchase_count"]) != 2 || prev["coverage"] != "completo" {
		t.Fatalf("anterior: %v", prev)
	}
	series := r.Body["series"].([]any)
	if len(series) != 14 {
		t.Fatalf("serie diaria: %d puntos", len(series))
	}
	var net, before int
	for _, p := range series {
		m := p.(map[string]any)
		net += fnum(m["net_cents"])
		before += fnum(m["previous_cents"])
	}
	if net != 17000 || before != 14000 || fnum(series[2].(map[string]any)["previous_cents"]) != 10000 {
		t.Fatalf("serie: %v", series)
	}
	changes := map[string]map[string]any{}
	for _, c := range r.Body["category_changes"].([]any) {
		m := c.(map[string]any)
		changes[m["id"].(string)] = m
	}
	// Comida: 16 000 contra 10 000 → +60 %. Ocio: 1 000 contra 4 000 → −75 %.
	if fnum(changes[comida]["diff_cents"]) != 6000 || changes[comida]["pct"].(float64) != 60 || changes[ocio]["pct"].(float64) != -75 {
		t.Fatalf("cambios: %v", changes)
	}
	merchants := r.Body["merchants"].([]any)
	top := merchants[0].(map[string]any)
	if len(merchants) != 2 || top["name"] != "Supermaxi" || fnum(top["spent_cents"]) != 16000 || fnum(top["purchase_count"]) != 2 || fnum(top["previous_cents"]) != 10000 {
		t.Fatalf("comercios: %v", merchants)
	}

	// Filtro por cuenta: la cuenta fusionada cuenta con su principal, se pida por cualquiera de las dos.
	for _, id := range []string{visa, vieja} {
		f := a.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10&account="+id, nil)
		if fnum(f.Body["total_cents"]) != 16000 || f.Body["filters"].(map[string]any)["account"] != visa || fnum(f.Body["previous"].(map[string]any)["total_cents"]) != 10000 {
			t.Fatalf("filtro de cuenta %s: %s", id, f.Raw)
		}
	}
	// Filtro por categoría principal: incluye la subcategoría.
	f := a.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10&category="+comida, nil)
	if fnum(f.Body["total_cents"]) != 16000 || len(f.Body["by_category"].([]any)) != 1 || len(f.Body["merchants"].([]any)) != 1 {
		t.Fatalf("filtro de categoría: %s", f.Raw)
	}
	a.must(400, "GET", "/api/reports/summary?category="+delivery, nil) // solo categorías principales
	a.must(400, "GET", "/api/reports/summary?compare=siempre", nil)
	// Aislamiento: nadie filtra por una cuenta o categoría ajena.
	b.must(400, "GET", "/api/reports/summary?account="+visa, nil)
	b.must(400, "GET", "/api/reports/summary?category="+comida, nil)
	b.must(400, "GET", "/api/reports/projection?account="+visa, nil)
	if rb := b.must(200, "GET", "/api/reports/summary", nil); len(rb.Body["merchants"].([]any)) != 0 || fnum(rb.Body["previous"].(map[string]any)["total_cents"]) != 0 {
		t.Fatalf("B ve datos de A: %s", rb.Raw)
	}

	// Mismo periodo del año anterior: sin datos, así que no hay porcentajes.
	y := a.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10&compare=year", nil)
	if yp := y.Body["previous"].(map[string]any); yp["from"] != "2025-10-01" || yp["coverage"] != "sin_datos" {
		t.Fatalf("año anterior: %v", yp)
	}
	for _, c := range y.Body["category_changes"].([]any) {
		if c.(map[string]any)["pct"] != nil {
			t.Fatalf("porcentaje sin base: %v", c)
		}
	}
	// Seis meses: semanas; doce: meses.
	if g := a.must(200, "GET", "/api/reports/summary?from=2026-05&to=2026-10", nil).Body["period"].(map[string]any)["granularity"]; g != "week" {
		t.Fatalf("granularidad: %v", g)
	}

	// Proyección filtrada por cuenta: solo los movimientos de esa cuenta.
	p := a.must(200, "GET", "/api/reports/projection?account="+efectivo, nil)
	if fnum(p.Body["spent_cents"]) != 1000 {
		t.Fatalf("proyección por cuenta: %s", p.Raw)
	}

	// La exportación aplica los mismos filtros.
	a.stepUp()
	csv := a.must(200, "GET", "/api/reports/export.csv?from=2026-09&to=2026-10&category="+ocio, nil).Raw
	if strings.Contains(csv, "Supermaxi") || strings.Count(csv, "Cine") != 2 {
		t.Fatalf("CSV filtrado: %s", csv)
	}
	csv = a.must(200, "GET", "/api/reports/export.csv?from=2026-10&to=2026-10&account="+visa, nil).Raw
	if strings.Contains(csv, "Cine") || strings.Count(csv, "upermaxi") != 3 {
		t.Fatalf("CSV por cuenta: %s", csv)
	}
}

// El día de cada gasto sale de la zona horaria del usuario: el mismo instante cae en días distintos.
func TestReportesZonaHoraria(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct{ tz, day string }{{"America/Guayaquil", "2026-10-09"}, {"Asia/Tokyo", "2026-10-10"}} {
		c := e.signup(strings.ToLower(strings.ReplaceAll(tc.tz, "/", "")) + "@x.com")
		c.must(204, "PATCH", "/api/me", map[string]any{"timezone": tc.tz})
		acc, cat := c.firstID("/api/accounts"), c.categoryID("Comida")
		c.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": 700, "occurred_at": time.Date(2026, 10, 10, 3, 0, 0, 0, time.UTC).Format(time.RFC3339)})
		r := c.must(200, "GET", "/api/reports/summary?from=2026-10&to=2026-10", nil)
		for _, p := range r.Body["series"].([]any) {
			m := p.(map[string]any)
			if fnum(m["net_cents"]) != 0 && m["start"] != tc.day {
				t.Fatalf("%s: gasto en %s, se esperaba %s", tc.tz, m["start"], tc.day)
			}
		}
	}
}

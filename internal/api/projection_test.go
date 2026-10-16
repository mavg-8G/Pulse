package api_test

import (
	"testing"
	"time"
)

func TestProyeccionDeFinDeMes(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@example.com")
	b := e.signup("beto@example.com")
	acc := a.firstID("/api/accounts")
	comida, hogar := a.categoryID("Comida"), a.categoryID("Hogar")
	delivery := a.must(201, "POST", "/api/categories", map[string]any{"name": "Delivery", "parent_id": comida}).str("id")
	tx := func(cat string, cents int, when string, kind string) {
		a.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": cents, "occurred_at": when, "kind": kind})
	}
	tx(comida, 9000, "2026-09-01T12:00", "purchase")
	tx(delivery, 3000, "2026-10-02T20:00", "purchase") // subcategoría: suma a Comida
	tx(comida, 1000, "2026-10-05T09:00", "refund")     // devolución: resta
	tx(comida, 2000, "2026-10-10T13:00", "purchase")
	a.must(201, "POST", "/api/recurring", map[string]any{"account_id": acc, "category_id": hogar, "amount_cents": 50000, "merchant": "Arriendo", "start_date": "2026-10-28"})

	r := a.must(200, "GET", "/api/reports/projection", nil)
	if r.str("month") != "2026-10" || fnum(r.Body["day"]) != 14 || fnum(r.Body["days_in_month"]) != 31 || fnum(r.Body["spent_cents"]) != 4000 {
		t.Fatalf("mes: %s", r.Raw)
	}
	// Con un solo mes cerrado se usa el método actual y se avisa.
	if r.str("method") != "ritmo" || fnum(r.Body["projected_cents"]) != 8857 || fnum(r.Body["baseline_cents"]) != 8857 {
		t.Fatalf("método: %s", r.Raw)
	}
	if fnum(r.Body["upcoming_cents"]) != 50000 {
		t.Fatalf("el arriendo del 28 es un gasto fijo conocido: %s", r.Raw)
	}
	warnings := r.Body["warnings"].([]any)
	if len(warnings) == 0 || warnings[0] != "sin_meses_cerrados" {
		t.Fatalf("avisos: %v", warnings)
	}
	cats := map[string]map[string]any{}
	for _, c := range r.Body["by_category"].([]any) {
		m := c.(map[string]any)
		cats[m["id"].(string)] = m
	}
	if _, ok := cats[delivery]; ok || fnum(cats[comida]["spent_cents"]) != 4000 || fnum(cats[hogar]["upcoming_cents"]) != 50000 {
		t.Fatalf("categorías: %v", cats)
	}
	cum := r.Body["cumulative"].([]any)
	if len(cum) != 14 || fnum(cum[1]) != 3000 || fnum(cum[4]) != 2000 || fnum(cum[13]) != 4000 || len(r.Body["previous_cumulative"].([]any)) != 30 {
		t.Fatalf("acumulado: %v", cum)
	}
	if len(r.Body["accuracy"].([]any)) != 1 {
		t.Fatalf("septiembre se evalúa: %s", r.Raw)
	}

	// Aislamiento: otra persona no ve nada de lo anterior.
	rb := b.must(200, "GET", "/api/reports/projection", nil)
	if fnum(rb.Body["spent_cents"]) != 0 || fnum(rb.Body["upcoming_cents"]) != 0 || len(rb.Body["by_category"].([]any)) != 0 || rb.Body["warnings"].([]any)[0] != "sin_datos" {
		t.Fatalf("aislamiento: %s", rb.Raw)
	}
	e.client().must(401, "GET", "/api/reports/projection", nil)
}

func TestProyeccionZonaHorariaYCambioDeHorario(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ny@example.com")
	c.must(204, "PATCH", "/api/me", map[string]any{"timezone": "America/New_York"})
	acc, cat := c.firstID("/api/accounts"), c.categoryID("Comida")
	for _, w := range []string{"2026-10-31T23:30", "2026-11-01T00:30", "2026-11-01T23:30"} { // el 1 de noviembre dura 25 horas
		c.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": 100, "occurred_at": w})
	}
	e.clock.Set(time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)) // 10:00 en Nueva York (EST)
	c.login()
	r := c.must(200, "GET", "/api/reports/projection", nil)
	cum, prev := r.Body["cumulative"].([]any), r.Body["previous_cumulative"].([]any)
	if r.str("month") != "2026-11" || fnum(r.Body["day"]) != 2 || fnum(r.Body["spent_cents"]) != 200 || fnum(cum[0]) != 200 || fnum(prev[30]) != 100 {
		t.Fatalf("%s", r.Raw)
	}
}

func fnum(v any) int { f, _ := v.(float64); return int(f) }

func TestProyeccionSemanaYMesConRango(t *testing.T) {
	e := newEnv(t) // hoy: miércoles 14 oct 2026
	a := e.signup("ana@example.com")
	b := e.signup("beto@example.com")
	acc := a.firstID("/api/accounts")
	comida, hogar := a.categoryID("Comida"), a.categoryID("Hogar")
	for d := 1; d <= 40; d++ {
		when := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -d).Format("2006-01-02T15:04")
		a.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": comida, "amount_cents": 1000, "occurred_at": when})
	}
	// El arriendo del 28: cae en octubre (fuera de la próxima semana) y otra vez el 28 de noviembre.
	a.must(201, "POST", "/api/recurring", map[string]any{"account_id": acc, "category_id": hogar, "amount_cents": 50000, "merchant": "Arriendo", "start_date": "2026-10-28"})

	f := a.must(200, "GET", "/api/reports/summary", nil).Body["forecast"].(map[string]any)
	week, month := f["week_model"].(map[string]any), f["month_model"].(map[string]any)
	if week["from"] != "2026-10-19" || fnum(week["days"]) != 7 || fnum(week["projected_cents"]) != 7000 || fnum(week["upcoming_cents"]) != 0 {
		t.Fatalf("semana: %v", week)
	}
	if fnum(week["low_cents"]) != 7000 || fnum(week["high_cents"]) != 7000 || fnum(week["backtests"]) < 3 || week["coverage"].(float64) != 1 {
		t.Fatalf("rango de un gasto constante: %v", week)
	}
	if month["from"] != "2026-11-01" || fnum(month["projected_cents"]) != 80000 || fnum(month["upcoming_cents"]) != 50000 {
		t.Fatalf("mes con el arriendo de noviembre: %v", month)
	}
	cum := month["cumulative"].([]any)
	if len(cum) != 30 || fnum(cum[26]) != 27000 || fnum(cum[27]) != 78000 {
		t.Fatalf("el arriendo cae el 28 en la curva: %v", cum)
	}
	var sum int
	for _, c := range month["by_category"].([]any) {
		sum += fnum(c.(map[string]any)["projected_cents"])
	}
	if sum != 80000 {
		t.Fatalf("categorías suman %d", sum)
	}
	// Aislamiento: sin historial propio no hay modelo.
	if fb := b.must(200, "GET", "/api/reports/summary", nil).Body["forecast"].(map[string]any); fb["week_model"] != nil || fb["ready"] != false {
		t.Fatalf("beto: %v", fb)
	}
}

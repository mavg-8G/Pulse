package api_test

import (
	"fmt"
	"sort"
	"testing"
	"time"
)

type planClient struct {
	*client
	t   *testing.T
	acc string
}

func (p planClient) spend(acc, cat string, cents int, when string, kind ...string) string {
	b := map[string]any{"account_id": acc, "category_id": cat, "amount_cents": cents, "occurred_at": when}
	if len(kind) > 0 {
		b["kind"] = kind[0]
	}
	return p.must(201, "POST", "/api/transactions", b).str("id")
}

// alertsOf devuelve los umbrales registrados de un presupuesto en un mes.
func (p planClient) alertsOf(budgetID, month string) string {
	var out []float64
	for _, a := range p.must(200, "GET", "/api/alerts?month="+month, nil).List {
		m := a.(map[string]any)
		if m["budget_id"] == budgetID {
			out = append(out, m["threshold"].(float64))
		}
	}
	sort.Float64s(out)
	return fmt.Sprint(out)
}

func (p planClient) line(budgetID, month string) map[string]any {
	p.t.Helper()
	for _, x := range p.must(200, "GET", "/api/budgets/summary?month="+month, nil).Body["lines"].([]any) {
		if l := x.(map[string]any); l["budget_id"] == budgetID {
			return l
		}
	}
	p.t.Fatalf("no hay línea para %s en %s", budgetID, month)
	return nil
}

func cents(m map[string]any, k string) int { f, _ := m[k].(float64); return int(f) }

// Arrastre con reloj falso entre meses de 30, 31 y 28 días: el sobrante de un mes aparece en el siguiente
// y el exceso se descuenta; los gastos del último día a las 23:59 (hora local) cuentan en su mes.
func TestArrastreEntreMeses(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)) // septiembre: 30 días
	c := e.signup("ana@x.com")
	p := planClient{c, t, c.firstID("/api/accounts")}
	comida := c.categoryID("Comida")
	sub := c.must(201, "POST", "/api/categories", map[string]any{"name": "Delivery", "parent_id": comida}).str("id")
	b := c.must(201, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 10000, "rollover": true})
	if b.str("rollover_from") != "2026-09" || b.Body["rollover"] != true {
		t.Fatalf("arrastre desde el mes actual: %s", b.Raw)
	}
	id := b.str("id")
	p.spend(p.acc, comida, 5000, "2026-09-02")
	p.spend(p.acc, sub, 2000, "2026-09-30T23:59")        // subcategoría, último minuto de septiembre
	p.spend(p.acc, comida, 1000, "2026-09-20", "refund") // el reverso descuenta
	if l := p.line(id, "2026-09"); cents(l, "spent_cents") != 6000 || cents(l, "carry_cents") != 0 || cents(l, "budget_cents") != 10000 {
		t.Fatalf("septiembre: %v", l)
	}
	// Octubre (31 días): llegan +40 de septiembre; tope efectivo 140.
	e.clock.Set(time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC))
	c.login()
	l := p.line(id, "2026-10")
	if cents(l, "carry_cents") != 4000 || cents(l, "budget_cents") != 14000 || cents(l, "cap_cents") != 10000 {
		t.Fatalf("octubre: %v", l)
	}
	p.spend(p.acc, comida, 15000, "2026-10-31T23:59") // gasta 150 de 140 → exceso de 10
	if l := p.line(id, "2026-10"); l["status"] != "over" || cents(l, "remaining_cents") != -1000 {
		t.Fatalf("octubre excedido: %v", l)
	}
	if got := p.alertsOf(id, "2026-10"); got != "[50 80 100]" {
		t.Fatalf("avisos de octubre: %s", got)
	}
	// Noviembre: el exceso de 10 se descuenta; con tope propio de 50 para noviembre, quedan 40.
	c.must(200, "PUT", "/api/budgets/"+id+"/months/2026-11", map[string]any{"amount_cents": 5000})
	if l := p.line(id, "2026-11"); cents(l, "carry_cents") != -1000 || cents(l, "budget_cents") != 4000 || l["month_cap"] != true {
		t.Fatalf("noviembre: %v", l)
	}
	// Febrero bisiesto de 2028 (29 días): el gasto del 29 a las 23:30 cuenta en febrero y su sobrante llega a marzo.
	c2 := e.signup("beto@x.com")
	p2 := planClient{c2, t, c2.firstID("/api/accounts")}
	e.clock.Set(time.Date(2028, 2, 10, 15, 0, 0, 0, time.UTC))
	c2.login()
	casa := c2.categoryID("Hogar")
	b2 := c2.must(201, "POST", "/api/budgets", map[string]any{"category_id": casa, "amount_cents": 20000, "rollover": true}).str("id")
	p2.spend(p2.acc, casa, 2500, "2028-02-29T23:30")
	e.clock.Set(time.Date(2028, 3, 1, 15, 0, 0, 0, time.UTC))
	c2.login()
	if l := p2.line(b2, "2028-02"); cents(l, "spent_cents") != 2500 || cents(l, "days_in_month") != 29 {
		t.Fatalf("febrero bisiesto: %v", l)
	}
	if l := p2.line(b2, "2028-03"); cents(l, "carry_cents") != 17500 {
		t.Fatalf("marzo: %v", l)
	}
	// Desactivar el arrastre deja el tope general.
	c2.must(200, "PATCH", "/api/budgets/"+b2, map[string]any{"rollover": false})
	if l := p2.line(b2, "2028-03"); cents(l, "carry_cents") != 0 || cents(l, "budget_cents") != 20000 {
		t.Fatalf("sin arrastre: %v", l)
	}
	// Elegir desde qué mes: hasta 12 meses atrás y nunca en el futuro.
	c2.must(400, "PATCH", "/api/budgets/"+b2, map[string]any{"rollover_from": "2028-01"})
	c2.must(200, "PATCH", "/api/budgets/"+b2, map[string]any{"rollover": true, "rollover_from": "2028-02"})
	c2.must(400, "PATCH", "/api/budgets/"+b2, map[string]any{"rollover_from": "2028-04"})
	c2.must(400, "PATCH", "/api/budgets/"+b2, map[string]any{"rollover_from": "2027-02"})
	// Un mes de 28 días (febrero 2029): arrastre elegido desde febrero, gasto del 28 a las 23:59.
	e.clock.Set(time.Date(2029, 3, 5, 15, 0, 0, 0, time.UTC))
	c.login()
	transporte := c.categoryID("Transporte")
	p.spend(p.acc, transporte, 400, "2029-02-28T23:59")
	p.spend(p.acc, transporte, 999, "2029-03-01T00:00")
	b3 := c.must(201, "POST", "/api/budgets", map[string]any{"category_id": transporte, "amount_cents": 1000, "rollover": true, "rollover_from": "2029-02"}).str("id")
	if l := p.line(b3, "2029-02"); cents(l, "spent_cents") != 400 || cents(l, "days_in_month") != 28 {
		t.Fatalf("febrero 2029: %v", l)
	}
	if l := p.line(b3, "2029-03"); cents(l, "carry_cents") != 600 || cents(l, "budget_cents") != 1600 || cents(l, "spent_cents") != 999 {
		t.Fatalf("marzo 2029: %v", l)
	}
}

// Cada umbral se registra una vez por mes aunque el tope suba, baje o se cambien los umbrales.
func TestUmbralesUnaVezAunqueCambieElTope(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	p := planClient{c, t, c.firstID("/api/accounts")}
	comida := c.categoryID("Comida")
	id := c.must(201, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 10000, "thresholds": "30, 60"}).str("id")
	p.spend(p.acc, comida, 3500, "2026-10-02")
	if got := p.alertsOf(id, "2026-10"); got != "[30]" {
		t.Fatalf("umbral propio: %s", got)
	}
	c.must(200, "PATCH", "/api/budgets/"+id, map[string]any{"amount_cents": 5000}) // 70 % → cruza 60
	c.must(200, "PATCH", "/api/budgets/"+id, map[string]any{"amount_cents": 20000})
	c.must(200, "PATCH", "/api/budgets/"+id, map[string]any{"amount_cents": 5000})
	if got := p.alertsOf(id, "2026-10"); got != "[30 60]" {
		t.Fatalf("tras cambiar el tope: %s", got)
	}
	c.must(200, "PATCH", "/api/budgets/"+id, map[string]any{"thresholds": "30,60,90"})
	p.spend(p.acc, comida, 1000, "2026-10-03") // 90 % de 50
	p.spend(p.acc, comida, 1, "2026-10-04")
	if got := p.alertsOf(id, "2026-10"); got != "[30 60 90]" {
		t.Fatalf("umbral nuevo: %s", got)
	}
	// Un tope propio de octubre menor también cuenta una sola vez.
	c.must(200, "PUT", "/api/budgets/"+id+"/months/2026-10", map[string]any{"amount_cents": 1000})
	c.must(200, "PUT", "/api/budgets/"+id+"/months/2026-10", map[string]any{"amount_cents": 1001})
	if got := p.alertsOf(id, "2026-10"); got != "[30 60 90]" {
		t.Fatalf("tope del mes: %s", got)
	}
}

func TestPresupuestoPorCuenta(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	p := planClient{a, t, a.firstID("/api/accounts")}
	visa := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Visa", "type": "credit_card"}).str("id")
	comida, casa := a.categoryID("Comida"), a.categoryID("Hogar")
	a.must(400, "POST", "/api/budgets", map[string]any{"amount_cents": 100})
	a.must(400, "POST", "/api/budgets", map[string]any{"category_id": comida, "account_id": visa, "amount_cents": 100})
	b.must(400, "POST", "/api/budgets", map[string]any{"account_id": visa, "amount_cents": 100})
	id := a.must(201, "POST", "/api/budgets", map[string]any{"account_id": visa, "amount_cents": 10000}).str("id")
	a.must(409, "POST", "/api/budgets", map[string]any{"account_id": visa, "amount_cents": 500})
	cat := a.must(201, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 100000}).str("id")
	// La cuenta suma todas las categorías; el otro medio de pago no cuenta.
	p.spend(visa, comida, 3000, "2026-10-02")
	p.spend(visa, casa, 3000, "2026-10-03")
	p.spend(p.acc, comida, 9000, "2026-10-04")
	p.spend(visa, casa, 1000, "2026-10-05", "refund")
	l := p.line(id, "2026-10")
	if l["kind"] != "account" || l["name"] != "Visa" || cents(l, "spent_cents") != 5000 {
		t.Fatalf("línea de cuenta: %v", l)
	}
	if got := p.alertsOf(id, "2026-10"); got != "[50]" {
		t.Fatalf("avisos de la cuenta: %s", got)
	}
	// El total del plan solo suma presupuestos por categoría (las cuentas se cruzan con ellas).
	s := a.must(200, "GET", "/api/budgets/summary?month=2026-10", nil)
	if tot := s.Body["totals"].(map[string]any); cents(tot, "budget_cents") != 100000 || cents(tot, "spent_cents") != 12000 {
		t.Fatalf("total del plan: %s", s.Raw)
	}
	_ = cat
	// Aislamiento: B no ve ni toca el presupuesto de A.
	b.must(404, "PATCH", "/api/budgets/"+id, map[string]any{"amount_cents": 1})
	b.must(404, "PUT", "/api/budgets/"+id+"/months/2026-12", map[string]any{"amount_cents": 1})
	b.must(404, "DELETE", "/api/budgets/"+id, nil)
	if r := b.must(200, "GET", "/api/budgets/summary?month=2026-10", nil); len(r.Body["lines"].([]any)) != 0 {
		t.Fatal("B ve el plan de A")
	}
	if r := b.must(200, "GET", "/api/alerts?month=2026-10", nil); len(r.List) != 0 {
		t.Fatal("B ve avisos de A")
	}
	a.must(400, "PATCH", "/api/budgets/"+id, map[string]any{"category_id": comida})
	// Inicio usa el total del plan por categoría.
	if d := a.must(200, "GET", "/api/dashboard", nil); d.Body["has_budgets"] != true {
		t.Fatalf("inicio: %s", d.Raw)
	}
	a.must(204, "DELETE", "/api/budgets/"+cat, nil)
	if d := a.must(200, "GET", "/api/dashboard", nil); d.Body["has_budgets"] != false {
		t.Fatalf("inicio solo con presupuesto por cuenta: %s", d.Raw)
	}
	// Borrar la cuenta borra su presupuesto.
	a.must(204, "DELETE", "/api/budgets/"+id, nil)
}

func TestTopesPorMesYCopiarPlan(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	comida, casa := c.categoryID("Comida"), c.categoryID("Hogar")
	p := planClient{c, t, c.firstID("/api/accounts")}
	b1 := c.must(201, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 10000}).str("id")
	b2 := c.must(201, "POST", "/api/budgets", map[string]any{"category_id": casa, "amount_cents": 50000}).str("id")
	r := c.must(200, "PUT", "/api/budgets/"+b1+"/months/2026-12", map[string]any{"amount_cents": 30000})
	if r.Body["months"].(map[string]any)["2026-12"].(float64) != 30000 {
		t.Fatalf("tope de diciembre: %s", r.Raw)
	}
	if l := p.line(b1, "2026-12"); cents(l, "budget_cents") != 30000 || cents(l, "base_cents") != 10000 {
		t.Fatalf("diciembre: %v", l)
	}
	if l := p.line(b1, "2026-11"); cents(l, "budget_cents") != 10000 || l["month_cap"] != false {
		t.Fatalf("noviembre usa el general: %v", l)
	}
	c.must(400, "PUT", "/api/budgets/"+b1+"/months/2026-13", map[string]any{"amount_cents": 1})
	c.must(400, "PUT", "/api/budgets/"+b1+"/months/2026-12", map[string]any{"amount_cents": 0})
	c.must(404, "PUT", "/api/budgets/nada/months/2026-12", map[string]any{"amount_cents": 1})
	// Enero tenía un tope propio en Casa; al copiar diciembre → enero, Comida toma 300 y Casa vuelve al general.
	c.must(200, "PUT", "/api/budgets/"+b2+"/months/2027-01", map[string]any{"amount_cents": 1})
	if r := c.must(200, "POST", "/api/budgets/copy", map[string]any{"from": "2026-12", "to": "2027-01"}); cents(r.Body, "copied") != 2 {
		t.Fatalf("copia: %s", r.Raw)
	}
	if l := p.line(b1, "2027-01"); cents(l, "budget_cents") != 30000 {
		t.Fatalf("enero comida: %v", l)
	}
	if l := p.line(b2, "2027-01"); cents(l, "budget_cents") != 50000 || l["month_cap"] != false {
		t.Fatalf("enero casa: %v", l)
	}
	c.must(400, "POST", "/api/budgets/copy", map[string]any{"from": "2026-12", "to": "2026-12"})
	c.must(400, "POST", "/api/budgets/copy", map[string]any{"from": "x", "to": "2026-12"})
	// Quitar el tope propio con null.
	c.must(200, "PUT", "/api/budgets/"+b1+"/months/2026-12", map[string]any{"amount_cents": nil})
	if l := p.line(b1, "2026-12"); cents(l, "budget_cents") != 10000 {
		t.Fatalf("sin tope propio: %v", l)
	}
	// Otro usuario no puede copiar sobre el plan de A (solo toca el suyo).
	o := e.signup("otro@x.com")
	o.must(200, "POST", "/api/budgets/copy", map[string]any{"from": "2026-12", "to": "2027-01"})
	if l := p.line(b1, "2027-01"); cents(l, "budget_cents") != 30000 {
		t.Fatal("la copia de otro usuario tocó el plan de A")
	}
}

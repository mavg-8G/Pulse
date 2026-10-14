package api_test

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"testing"
	"time"
)

func TestAislamientoEntreUsuarios(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")

	accA := a.must(201, "POST", "/api/accounts", map[string]any{"name": "Pichincha Visa", "type": "credit_card", "card_suffix": "989"}).str("id")
	catA := a.categoryID("Comida")
	txA := a.must(201, "POST", "/api/transactions", map[string]any{"account_id": accA, "category_id": catA, "amount_cents": 1768, "occurred_at": "2026-10-12T18:24", "merchant": "Carl's Jr"}).str("id")
	budA := a.must(201, "POST", "/api/budgets", map[string]any{"category_id": catA, "amount_cents": 20000}).str("id")
	accB := b.firstID("/api/accounts")
	catB := b.categoryID("Comida")

	// B no ve nada de A en los listados
	for _, path := range []string{"/api/accounts", "/api/categories", "/api/budgets"} {
		r := b.must(200, "GET", path, nil)
		for _, x := range r.List {
			id := x.(map[string]any)["id"]
			if id == accA || id == catA || id == budA {
				t.Fatalf("%s de B incluye datos de A", path)
			}
		}
	}
	if r := b.must(200, "GET", "/api/transactions", nil); r.Body["total"].(float64) != 0 {
		t.Fatal("B ve movimientos de A")
	}

	// B no puede leer, editar ni borrar recursos de A: siempre 404 (no 403)
	cases := []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/transactions/" + txA, nil},
		{"PATCH", "/api/transactions/" + txA, map[string]any{"amount_cents": 1}},
		{"DELETE", "/api/transactions/" + txA, nil},
		{"POST", "/api/transactions/" + txA + "/confirm", map[string]any{}},
		{"PATCH", "/api/accounts/" + accA, map[string]any{"name": "hack"}},
		{"DELETE", "/api/accounts/" + accA, nil},
		{"PATCH", "/api/categories/" + catA, map[string]any{"name": "hack"}},
		{"DELETE", "/api/categories/" + catA, nil},
		{"PATCH", "/api/budgets/" + budA, map[string]any{"amount_cents": 1}},
		{"DELETE", "/api/budgets/" + budA, nil},
	}
	for _, c := range cases {
		if r := b.do(c.method, c.path, c.body); r.Code != 404 {
			t.Errorf("%s %s como B: %d (esperaba 404) %s", c.method, c.path, r.Code, r.Raw)
		}
	}

	// B no puede usar la cuenta/categoría de A en sus propios registros
	b.must(400, "POST", "/api/transactions", map[string]any{"account_id": accA, "category_id": catB, "amount_cents": 100, "occurred_at": "2026-10-12"})
	b.must(400, "POST", "/api/transactions", map[string]any{"account_id": accB, "category_id": catA, "amount_cents": 100, "occurred_at": "2026-10-12"})
	b.must(400, "POST", "/api/budgets", map[string]any{"category_id": catA, "amount_cents": 100})
	b.must(400, "POST", "/api/categories", map[string]any{"name": "Sub", "parent_id": catA})
	txB := b.must(201, "POST", "/api/transactions", map[string]any{"account_id": accB, "category_id": catB, "amount_cents": 100, "occurred_at": "2026-10-12"}).str("id")
	b.must(400, "PATCH", "/api/transactions/"+txB, map[string]any{"account_id": accA})
	b.must(400, "PATCH", "/api/transactions/"+txB, map[string]any{"category_id": catA})

	// Sesiones y passkeys de A no se pueden revocar desde B
	sessA := a.firstID("/api/security/sessions")
	b.must(404, "DELETE", "/api/security/sessions/"+sessA, nil)
	b.stepUp()
	b.must(404, "DELETE", "/api/security/passkeys/"+sessA, nil)

	// Lo de A sigue intacto
	r := a.must(200, "GET", "/api/transactions/"+txA, nil)
	if r.Body["amount_cents"].(float64) != 1768 || r.str("merchant") != "Carl's Jr" {
		t.Fatalf("el movimiento de A cambió: %s", r.Raw)
	}
	a.must(200, "GET", "/api/me", nil)

	// La BD también lo impide aunque el código fallara: FK compuesta (id, user_id)
	uB, _ := e.st.UserByEmail(context.Background(), "beto@x.com")
	_, err := e.st.DB.Exec(`INSERT INTO transactions (id, user_id, account_id, category_id, amount_cents, occurred_at, created_at, updated_at)
		VALUES ('x', ?, ?, ?, 1, '2026-10-01T00:00:00Z', 'x', 'x')`, uB.ID, accA, catA)
	if err == nil {
		t.Fatal("la base de datos aceptó un movimiento con la cuenta de otro usuario")
	}
}

func TestMovimientos(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	acc := c.must(201, "POST", "/api/accounts", map[string]any{"name": "Produbanco Visa", "type": "credit_card", "card_suffix": "7828", "institution": "Produbanco"}).str("id")
	comida := c.categoryID("Comida")
	tx := func(amount int, when, merchant string, extra ...any) map[string]any {
		m := map[string]any{"account_id": acc, "category_id": comida, "amount_cents": amount, "occurred_at": when, "merchant": merchant}
		for i := 0; i+1 < len(extra); i += 2 {
			m[extra[i].(string)] = extra[i+1]
		}
		return m
	}

	t.Run("validación", func(t *testing.T) {
		bad := []map[string]any{
			tx(0, "2026-10-12", "x"),
			tx(-500, "2026-10-12", "x"),
			tx(100_000_001, "2026-10-12", "x"),
			tx(100, "12/10/2026", "x"),
			tx(100, "1999-12-31", "x"),
			tx(100, "2028-01-01", "x"),
			tx(100, "2026-10-12", string(bytes.Repeat([]byte("a"), 121))),
			tx(100, "2026-10-12", "x", "kind", "transfer"),
			tx(100, "2026-10-12", "x", "status", "hacked"),
			{"category_id": comida, "amount_cents": 100, "occurred_at": "2026-10-12"},
		}
		for i, b := range bad {
			if r := c.do("POST", "/api/transactions", b); r.Code != 400 {
				t.Errorf("caso %d: esperaba 400, obtuve %d %s", i, r.Code, r.Raw)
			}
		}
		// 1.000.000,00 exacto sí
		c.must(201, "POST", "/api/transactions", tx(100_000_000, "2026-09-01", "Auto"))
	})

	t.Run("cifrado en reposo", func(t *testing.T) {
		c.must(201, "POST", "/api/transactions", tx(434, "2026-10-08T16:25", "PEDIDOSYA GROCERIES", "notes", "nota secreta"))
		rows, _ := e.st.DB.Query(`SELECT merchant_enc, notes_enc FROM transactions`)
		defer rows.Close()
		for rows.Next() {
			var m, n []byte
			rows.Scan(&m, &n)
			if bytes.Contains(m, []byte("PEDIDOSYA")) || bytes.Contains(n, []byte("secreta")) {
				t.Fatal("datos sensibles en texto plano en la BD")
			}
		}
	})

	t.Run("mes en zona local: 31 a las 23:30 es octubre", func(t *testing.T) {
		id := c.must(201, "POST", "/api/transactions", tx(999, "2026-10-31T23:30", "Medianoche")).str("id")
		oct := c.must(200, "GET", "/api/transactions?month=2026-10&q=medianoche", nil)
		nov := c.must(200, "GET", "/api/transactions?month=2026-11&q=medianoche", nil)
		if oct.Body["total"].(float64) != 1 || nov.Body["total"].(float64) != 0 {
			t.Fatalf("octubre=%v noviembre=%v", oct.Body["total"], nov.Body["total"])
		}
		r := c.must(200, "GET", "/api/transactions/"+id, nil)
		if r.str("occurred_at") != "2026-11-01T04:30:00Z" {
			t.Fatalf("se guarda en UTC: %s", r.str("occurred_at"))
		}
	})

	t.Run("búsqueda sin tildes ni mayúsculas", func(t *testing.T) {
		c.must(201, "POST", "/api/transactions", tx(350, "2026-10-10", "Café Galletti"))
		r := c.must(200, "GET", "/api/transactions?q=CAFE", nil)
		if r.Body["total"].(float64) != 1 {
			t.Fatalf("esperaba 1 resultado: %s", r.Raw)
		}
	})

	t.Run("filtro por categoría incluye subcategorías y suma con reversos", func(t *testing.T) {
		sub := c.must(201, "POST", "/api/categories", map[string]any{"name": "Delivery", "parent_id": comida}).str("id")
		c.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": sub, "amount_cents": 812, "occurred_at": "2026-10-05", "merchant": "Uber Eats"})
		c.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": sub, "amount_cents": 200, "occurred_at": "2026-10-06", "kind": "refund", "merchant": "Uber Eats"})
		all := c.must(200, "GET", "/api/transactions?month=2026-10&category_id="+comida, nil)
		onlySub := c.must(200, "GET", "/api/transactions?month=2026-10&category_id="+sub, nil)
		if onlySub.Body["total"].(float64) != 2 || onlySub.Body["sum_cents"].(float64) != 612 {
			t.Fatalf("subcategoría: %s", onlySub.Raw)
		}
		if all.Body["total"].(float64) <= onlySub.Body["total"].(float64) {
			t.Fatal("el padre debe incluir los de la subcategoría y los propios")
		}
	})

	t.Run("paginación", func(t *testing.T) {
		r := c.must(200, "GET", "/api/transactions?limit=2&offset=1", nil)
		if len(r.Body["items"].([]any)) != 2 || r.Body["total"].(float64) < 5 {
			t.Fatalf("paginación: %s", r.Raw)
		}
		r = c.must(200, "GET", "/api/transactions?limit=2&offset=999", nil)
		if len(r.Body["items"].([]any)) != 0 {
			t.Fatal("offset fuera de rango debe dar lista vacía")
		}
		c.must(400, "GET", "/api/transactions?month=2026-13", nil)
	})

	t.Run("cuenta archivada no admite gastos nuevos", func(t *testing.T) {
		arch := c.must(201, "POST", "/api/accounts", map[string]any{"name": "Vieja", "type": "bank"}).str("id")
		c.must(200, "PATCH", "/api/accounts/"+arch, map[string]any{"archived": true})
		c.must(400, "POST", "/api/transactions", map[string]any{"account_id": arch, "category_id": comida, "amount_cents": 100, "occurred_at": "2026-10-12"})
	})

	t.Run("editar, confirmar y borrar", func(t *testing.T) {
		id := c.must(201, "POST", "/api/transactions", tx(100, "2026-10-12", "Tienda", "status", "pending_review")).str("id")
		r := c.must(200, "PATCH", "/api/transactions/"+id, map[string]any{"amount_cents": 150, "notes": "ajustado"})
		if r.Body["amount_cents"].(float64) != 150 || r.str("merchant") != "Tienda" {
			t.Fatalf("edición parcial: %s", r.Raw)
		}
		if r := c.must(200, "POST", "/api/transactions/"+id+"/confirm", map[string]any{}); r.str("status") != "confirmed" {
			t.Fatal("no se confirmó")
		}
		c.must(204, "DELETE", "/api/transactions/"+id, nil)
		c.must(404, "GET", "/api/transactions/"+id, nil)
	})
}

func TestCuentasYCategorias(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")

	t.Run("validación de cuentas", func(t *testing.T) {
		for _, b := range []map[string]any{
			{"name": "", "type": "bank"},
			{"name": "x", "type": "crypto"},
			{"name": "x", "type": "credit_card", "card_suffix": "12"},
			{"name": "x", "type": "credit_card", "card_suffix": "12345"},
			{"name": "x", "type": "credit_card", "card_suffix": "12a4"},
			{"name": "x", "type": "bank", "color": "red"},
			{"name": "x", "type": "bank", "currency": "dolar"},
		} {
			if r := c.do("POST", "/api/accounts", b); r.Code != 400 {
				t.Errorf("%v: esperaba 400, obtuve %d", b, r.Code)
			}
		}
		// Sufijo de 3 dígitos (Pichincha, Diners) es válido
		c.must(201, "POST", "/api/accounts", map[string]any{"name": "Diners Titanium", "type": "credit_card", "card_suffix": "666", "institution": "Diners Club"})
	})

	t.Run("cuenta con movimientos no se borra, se archiva", func(t *testing.T) {
		acc := c.must(201, "POST", "/api/accounts", map[string]any{"name": "Pichincha", "type": "credit_card", "card_suffix": "989"}).str("id")
		c.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": c.categoryID("Ocio"), "amount_cents": 100, "occurred_at": "2026-10-12"})
		c.must(409, "DELETE", "/api/accounts/"+acc, nil)
		empty := c.must(201, "POST", "/api/accounts", map[string]any{"name": "Vacía", "type": "wallet"}).str("id")
		c.must(204, "DELETE", "/api/accounts/"+empty, nil)
	})

	t.Run("borrar categoría mueve sus gastos a Otros", func(t *testing.T) {
		acc := c.firstID("/api/accounts")
		cat := c.must(201, "POST", "/api/categories", map[string]any{"name": "Mascotas", "icon": "🐶", "color": "#aa5500"}).str("id")
		id := c.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": 2500, "occurred_at": "2026-10-12"}).str("id")
		r := c.must(200, "DELETE", "/api/categories/"+cat, nil)
		if r.Body["moved_transactions"].(float64) != 1 {
			t.Fatalf("esperaba mover 1: %s", r.Raw)
		}
		if got := c.must(200, "GET", "/api/transactions/"+id, nil).str("category_id"); got != c.categoryID("Otros") {
			t.Fatal("el gasto no quedó en Otros")
		}
	})

	t.Run("reglas de jerarquía", func(t *testing.T) {
		c.must(409, "DELETE", "/api/categories/"+c.categoryID("Otros"), nil)
		parent := c.categoryID("Hogar")
		sub := c.must(201, "POST", "/api/categories", map[string]any{"name": "Luz", "parent_id": parent}).str("id")
		c.must(400, "POST", "/api/categories", map[string]any{"name": "Nieto", "parent_id": sub})
		c.must(409, "DELETE", "/api/categories/"+parent, nil)
		c.must(400, "PATCH", "/api/categories/"+parent, map[string]any{"parent_id": c.categoryID("Servicios")})
		c.must(400, "PATCH", "/api/categories/"+parent, map[string]any{"parent_id": parent})
		c.must(409, "POST", "/api/categories", map[string]any{"name": "comida"})                      // nombre duplicado sin importar mayúsculas
		c.must(201, "POST", "/api/categories", map[string]any{"name": "Comida", "parent_id": parent}) // en otro padre sí
	})
}

func TestPresupuestosYAlertas(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	acc := c.firstID("/api/accounts")
	comida := c.categoryID("Comida")
	sub := c.must(201, "POST", "/api/categories", map[string]any{"name": "Delivery", "parent_id": comida}).str("id")
	spend := func(cat string, cents int, when string, kind ...string) string {
		b := map[string]any{"account_id": acc, "category_id": cat, "amount_cents": cents, "occurred_at": when}
		if len(kind) > 0 {
			b["kind"] = kind[0]
		}
		return c.must(201, "POST", "/api/transactions", b).str("id")
	}
	alerts := func(month string) []float64 {
		var out []float64
		for _, a := range c.must(200, "GET", "/api/alerts?month="+month, nil).List {
			out = append(out, a.(map[string]any)["threshold"].(float64))
		}
		sort.Float64s(out)
		return out
	}

	c.must(400, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 0})
	c.must(400, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 100, "thresholds": "0,300"})
	bud := c.must(201, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 10000}).str("id")
	c.must(409, "POST", "/api/budgets", map[string]any{"category_id": comida, "amount_cents": 5000})

	spend(comida, 4999, "2026-10-02")
	if a := alerts("2026-10"); len(a) != 0 {
		t.Fatalf("49,99%% no cruza nada: %v", a)
	}
	spend(sub, 1, "2026-10-03") // la subcategoría cuenta para el padre → 50 % exacto
	if a := alerts("2026-10"); fmt.Sprint(a) != "[50]" {
		t.Fatalf("esperaba [50]: %v", a)
	}
	big := spend(comida, 3000, "2026-10-04") // 80 %
	if a := alerts("2026-10"); len(a) != 2 {
		t.Fatalf("esperaba 50 y 80: %v", a)
	}
	// Borrar y volver a cruzar no duplica el aviso
	c.must(204, "DELETE", "/api/transactions/"+big, nil)
	spend(comida, 3000, "2026-10-05")
	if a := alerts("2026-10"); len(a) != 2 {
		t.Fatalf("los avisos no deben repetirse en el mismo mes: %v", a)
	}
	// Un reverso resta
	spend(comida, 3000, "2026-10-06", "refund")
	// Otro mes es independiente
	spend(comida, 9000, "2026-11-02")
	if a := alerts("2026-11"); fmt.Sprint(a) != "[50 80]" {
		t.Fatalf("noviembre: %v", a)
	}

	// Resumen a mitad de octubre (reloj: 14 oct): gastado 4999+1+3000-3000 = 5000
	s := c.must(200, "GET", "/api/budgets/summary?month=2026-10", nil)
	line := s.Body["lines"].([]any)[0].(map[string]any)
	if line["spent_cents"].(float64) != 5000 || line["percent"].(float64) != 50 || line["days_elapsed"].(float64) != 14 {
		t.Fatalf("resumen: %s", s.Raw)
	}
	// Proyección: 5000 * 31 / 14 = 11071,4 → 11071
	if line["projected_cents"].(float64) != 11071 || line["projected_over_cents"].(float64) != 1071 || line["status"] != "warning" {
		t.Fatalf("proyección: %v", line)
	}
	// Bajar el presupuesto dispara el 100 % en el mes actual
	c.must(200, "PATCH", "/api/budgets/"+bud, map[string]any{"amount_cents": 4000})
	if a := alerts("2026-10"); len(a) != 3 {
		t.Fatalf("al bajar el presupuesto debe registrar el 100%%: %v", a)
	}

	// Inicio
	d := c.must(200, "GET", "/api/dashboard?month=2026-10", nil)
	if d.Body["spent_cents"].(float64) != 5000 || len(d.Body["daily_cents"].([]any)) != 31 || d.Body["has_budgets"] != true {
		t.Fatalf("dashboard: %s", d.Raw)
	}
	byCat := d.Body["by_category"].([]any)
	if len(byCat) != 1 || byCat[0].(map[string]any)["spent_cents"].(float64) != 5000 {
		t.Fatalf("por categoría (subcategoría agregada al padre): %v", byCat)
	}

	// Mes por defecto = mes actual del usuario
	e.clock.Set(time.Date(2026, 11, 1, 4, 30, 0, 0, time.UTC)) // 31 oct 23:30 en Guayaquil
	c.must(401, "GET", "/api/me", nil)                         // 17 días sin actividad: la sesión caducó
	c.login()
	if m := c.must(200, "GET", "/api/budgets/summary", nil).str("month"); m != "2026-10" {
		t.Fatalf("mes actual en zona local: %s", m)
	}
	// Borrar la categoría borra su presupuesto (y sus avisos)
	c.must(204, "DELETE", "/api/budgets/"+bud, nil)
	c.must(404, "PATCH", "/api/budgets/"+bud, map[string]any{"amount_cents": 1})
}

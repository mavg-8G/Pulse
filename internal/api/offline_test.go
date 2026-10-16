package api_test

import (
	"testing"
)

const (
	op1 = "0192f3a4-5b6c-7d8e-9f01-000000000001"
	op2 = "0192f3a4-5b6c-7d8e-9f01-000000000002"
	op3 = "0192f3a4-5b6c-7d8e-9f01-000000000003"
)

func ver(n int) reqOpt      { return withHeader("If-Match", `"`+itoa(n)+`"`) }
func opKey(k string) reqOpt { return withHeader("Idempotency-Key", k) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func num(r *resp, k string) int { f, _ := r.Body[k].(float64); return int(f) }

func current(r *resp) map[string]any { m, _ := r.Body["current"].(map[string]any); return m }

// Edición sin conexión → reconexión: el reenvío de la misma operación no aplica el cambio dos veces
// ni se confunde con un conflicto, aunque su If-Match ya sea viejo.
func TestEdicionEnColaSeAplicaUnaVez(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	tx := a.must(201, "POST", "/api/transactions", map[string]any{"account_id": a.firstID("/api/accounts"), "category_id": a.categoryID("Comida"), "amount_cents": 1000, "occurred_at": "2026-10-13T12:00"})
	if num(tx, "version") != 1 {
		t.Fatalf("versión inicial: %s", tx.Raw)
	}
	id := tx.str("id")
	r := a.must(200, "PATCH", "/api/transactions/"+id, map[string]any{"amount_cents": 1500}, ver(1), opKey(op1))
	if num(r, "version") != 2 || num(r, "amount_cents") != 1500 {
		t.Fatalf("edición: %s", r.Raw)
	}
	// Se perdió la respuesta y la cola la reenvía tal cual.
	r = a.must(200, "PATCH", "/api/transactions/"+id, map[string]any{"amount_cents": 1500}, ver(1), opKey(op1))
	if num(r, "version") != 2 {
		t.Fatalf("el reenvío aplicó otra vez el cambio: %s", r.Raw)
	}
	// Una operación distinta con la versión vieja sí es un conflicto.
	r = a.must(409, "PATCH", "/api/transactions/"+id, map[string]any{"amount_cents": 99}, ver(1), opKey(op2))
	if r.errCode() != "version_conflict" || current(r)["amount_cents"].(float64) != 1500 || current(r)["version"].(float64) != 2 {
		t.Fatalf("conflicto: %s", r.Raw)
	}
	if got := a.must(200, "GET", "/api/transactions/"+id, nil); num(got, "amount_cents") != 1500 {
		t.Fatalf("el conflicto pisó el dato: %s", got.Raw)
	}
	// Sin If-Match (la app con red) sigue funcionando como antes.
	a.must(200, "PATCH", "/api/transactions/"+id, map[string]any{"notes": "con red"})
	// Encabezados mal formados se rechazan.
	a.must(400, "PATCH", "/api/transactions/"+id, map[string]any{"notes": "x"}, withHeader("If-Match", "abc"))
	a.must(400, "PATCH", "/api/transactions/"+id, map[string]any{"notes": "x"}, withHeader("If-Match", "0"))
	a.must(400, "PATCH", "/api/transactions/"+id, map[string]any{"notes": "x"}, opKey("no-es-uuid"))
}

// Dos dispositivos: el teléfono edita sin conexión mientras el computador cambia o borra el mismo gasto.
func TestConflictosEntreDispositivos(t *testing.T) {
	e := newEnv(t)
	pc := e.signup("ana@x.com")
	phone := e.client()
	phone.email, phone.pw, phone.totp = pc.email, pc.pw, pc.totp
	phone.login()
	acc, cat := pc.firstID("/api/accounts"), pc.categoryID("Comida")
	id := pc.must(201, "POST", "/api/transactions", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": 1000, "occurred_at": "2026-10-13T12:00"}).str("id")

	// El computador cambia el monto; la edición en cola del teléfono (versión 1) choca y recibe lo actual.
	pc.must(200, "PATCH", "/api/transactions/"+id, map[string]any{"amount_cents": 2000}, ver(1))
	r := phone.must(409, "PATCH", "/api/transactions/"+id, map[string]any{"merchant": "Café"}, ver(1), opKey(op1))
	cur := current(r)
	if cur["amount_cents"].(float64) != 2000 || cur["version"].(float64) != 2 {
		t.Fatalf("lo actual: %s", r.Raw)
	}
	// El usuario elige "usar la mía" sobre la versión actual: se aplica una sola vez.
	r = phone.must(200, "PATCH", "/api/transactions/"+id, map[string]any{"merchant": "Café"}, ver(2), opKey(op2))
	if r.str("merchant") != "Café" || num(r, "amount_cents") != 2000 || num(r, "version") != 3 {
		t.Fatalf("resolución: %s", r.Raw)
	}
	// Borrar con versión vieja no borra lo que otro cambió.
	r = phone.must(409, "DELETE", "/api/transactions/"+id, nil, ver(2))
	if current(r)["version"].(float64) != 3 {
		t.Fatalf("borrado en conflicto: %s", r.Raw)
	}
	pc.must(200, "GET", "/api/transactions/"+id, nil)

	// Borrado concurrente: el computador lo borra; la edición y el borrado en cola del teléfono reciben 404
	// (la cola los da por resueltos) y nada reaparece.
	pc.must(204, "DELETE", "/api/transactions/"+id, nil, ver(3))
	phone.must(404, "PATCH", "/api/transactions/"+id, map[string]any{"notes": "tarde"}, ver(3), opKey(op3))
	phone.must(404, "DELETE", "/api/transactions/"+id, nil, ver(3))
	if r := pc.must(200, "GET", "/api/transactions", nil); r.Body["total"].(float64) != 0 {
		t.Fatalf("reapareció: %s", r.Raw)
	}
}

// Confirmar un gasto "por revisar" desde la cola es idempotente.
func TestConfirmarDesdeLaColaEsIdempotente(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	id := a.must(201, "POST", "/api/transactions", map[string]any{"account_id": a.firstID("/api/accounts"), "category_id": a.categoryID("Comida"), "amount_cents": 1000, "occurred_at": "2026-10-13T12:00", "status": "pending_review"}).str("id")
	a.must(200, "POST", "/api/transactions/"+id+"/confirm", map[string]any{}, ver(1), opKey(op1))
	r := a.must(200, "POST", "/api/transactions/"+id+"/confirm", map[string]any{}, ver(1), opKey(op1))
	if r.str("status") != "confirmed" || num(r, "version") != 2 {
		t.Fatalf("confirmación repetida: %s", r.Raw)
	}
}

func TestOtroUsuarioNoPuedeUsarLaCola(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	id := a.must(201, "POST", "/api/transactions", map[string]any{"account_id": a.firstID("/api/accounts"), "category_id": a.categoryID("Comida"), "amount_cents": 1000, "occurred_at": "2026-10-13T12:00"}).str("id")
	rec := a.must(201, "POST", "/api/recurring", map[string]any{"account_id": a.firstID("/api/accounts"), "category_id": a.categoryID("Servicios"), "amount_cents": 999, "start_date": "2026-11-05"}).str("id")
	for _, c := range []struct{ m, p string }{{"PATCH", "/api/transactions/" + id}, {"DELETE", "/api/transactions/" + id}, {"PATCH", "/api/recurring/" + rec}, {"DELETE", "/api/recurring/" + rec}} {
		var body any
		if c.m == "PATCH" {
			body = map[string]any{"amount_cents": 1}
		}
		if r := b.do(c.m, c.p, body, ver(1), opKey(op1)); r.Code != 404 || r.Body["current"] != nil {
			t.Errorf("%s %s como B: %d %s", c.m, c.p, r.Code, r.Raw)
		}
	}
	if r := a.must(200, "GET", "/api/transactions/"+id, nil); num(r, "version") != 1 {
		t.Fatalf("B cambió el gasto de A: %s", r.Raw)
	}
}

func TestGastosFijosSinConexion(t *testing.T) {
	e := newEnv(t) // hoy: 14 oct 2026
	a := e.signup("ana@x.com")
	acc, cat := a.firstID("/api/accounts"), a.categoryID("Servicios")
	body := map[string]any{"id": op1, "account_id": acc, "category_id": cat, "amount_cents": 1999, "merchant": "Netflix", "start_date": "2026-11-20"}
	r := a.must(201, "POST", "/api/recurring", body)
	if num(r, "version") != 1 || r.str("id") != op1 {
		t.Fatalf("alta: %s", r.Raw)
	}
	a.must(200, "POST", "/api/recurring", body) // reenvío desde la cola
	if l := a.must(200, "GET", "/api/recurring", nil).List; len(l) != 1 {
		t.Fatalf("se duplicó: %v", l)
	}
	a.must(400, "POST", "/api/recurring", map[string]any{"id": "x", "account_id": acc, "category_id": cat, "amount_cents": 1, "start_date": "2026-11-20"})

	// Edición: monto y día. El día 25 deja el próximo cobro el 25 de noviembre.
	r = a.must(200, "PATCH", "/api/recurring/"+op1, map[string]any{"amount_cents": 2499, "day": 25}, ver(1), opKey(op2))
	if num(r, "amount_cents") != 2499 || r.str("next_date") != "2026-11-25" || num(r, "day") != 25 || num(r, "version") != 2 {
		t.Fatalf("edición: %s", r.Raw)
	}
	a.must(200, "PATCH", "/api/recurring/"+op1, map[string]any{"amount_cents": 2499, "day": 25}, ver(1), opKey(op2)) // reenvío
	r = a.must(409, "PATCH", "/api/recurring/"+op1, map[string]any{"merchant": "Disney"}, ver(1), opKey(op3))
	if current(r)["amount_cents"].(float64) != 2499 {
		t.Fatalf("conflicto: %s", r.Raw)
	}
	// Día 31 en un mes de 30: el 30 de noviembre.
	if r := a.must(200, "PATCH", "/api/recurring/"+op1, map[string]any{"day": 31}); r.str("next_date") != "2026-11-30" {
		t.Fatalf("día 31: %s", r.Raw)
	}
	a.must(400, "PATCH", "/api/recurring/"+op1, map[string]any{"day": 32})
	a.must(400, "PATCH", "/api/recurring/"+op1, map[string]any{"start_date": "2026-12-01"})
	a.must(400, "PATCH", "/api/recurring/"+op1, map[string]any{"amount_cents": 0})

	// Cambiar a un día que ya pasó este mes, con el cobro de octubre pendiente: se registra al guardar.
	pend := a.must(201, "POST", "/api/recurring", map[string]any{"account_id": acc, "category_id": cat, "amount_cents": 700, "merchant": "Gym", "start_date": "2026-10-28"}).str("id")
	r = a.must(200, "PATCH", "/api/recurring/"+pend, map[string]any{"day": 10})
	if r.str("next_date") != "2026-11-10" {
		t.Fatalf("día pasado: %s", r.Raw)
	}
	if got := a.must(200, "GET", "/api/transactions?category_id="+cat, nil); got.Body["total"].(float64) != 1 {
		t.Fatalf("esperaba el cobro de octubre: %s", got.Raw)
	}

	// Borrado con versión vieja: conflicto; con la actual: hecho; repetido: 404 (la cola lo da por resuelto).
	a.must(409, "DELETE", "/api/recurring/"+op1, nil, ver(1))
	a.must(204, "DELETE", "/api/recurring/"+op1, nil, ver(3))
	a.must(404, "DELETE", "/api/recurring/"+op1, nil, ver(3))
}

package api_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func eml(t *testing.T, name string) string {
	b, err := os.ReadFile("../mailparse/testdata/" + name + ".eml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (c *client) importEML(t *testing.T, name string) *resp {
	t.Helper()
	return c.do("POST", "/api/import/eml", eml(t, name), withContentType("message/rfc822"))
}

func TestImportarCorreos(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")

	r := c.importEML(t, "pichincha-consumo")
	if r.Code != 200 || r.str("status") != "created" {
		t.Fatalf("%d %s", r.Code, r.Raw)
	}
	tx := r.Body["transaction"].(map[string]any)
	if tx["amount_cents"].(float64) != 1768 || tx["merchant"] != "Carl S Alban Borja" || tx["status"] != "pending_review" || tx["source"] != "email" {
		t.Fatalf("transacción: %v", tx)
	}
	// Cuenta creada automáticamente con banco y últimos dígitos; categoría por diccionario
	if r.str("account") != "Banco Pichincha …989" || r.Body["new_account"] != true || r.str("category") != "Comida" || r.str("category_by") != "builtin" {
		t.Fatalf("asignación: %s", r.Raw)
	}
	// El mismo correo otra vez no se duplica
	if d := c.importEML(t, "pichincha-consumo"); d.str("status") != "duplicate" {
		t.Fatalf("duplicado: %s", d.Raw)
	}
	// Segundo correo de la misma tarjeta: reutiliza la cuenta
	if r2 := c.importEML(t, "pichincha-consumo-latin1"); r2.Body["new_account"] == true || r2.str("account") != "Banco Pichincha …989" {
		t.Fatalf("debió reutilizar la cuenta: %s", r2.Raw)
	}
	// No-gastos y plantillas desconocidas no crean nada
	for name, want := range map[string]string{"pichincha-pago": "ignored", "diners-plantilla-nueva": "unrecognized", "desconocido": "unknown"} {
		if x := c.importEML(t, name); x.str("status") != want || x.Body["transaction"] != nil {
			t.Errorf("%s: %s", name, x.Raw)
		}
	}
	list := c.must(200, "GET", "/api/transactions?status=pending_review", nil)
	if list.Body["total"].(float64) != 2 {
		t.Fatalf("deben quedar 2 por revisar: %s", list.Raw)
	}
	// Confirmar
	c.must(200, "POST", "/api/transactions/"+tx["id"].(string)+"/confirm", map[string]any{})

	// Entrada inválida
	if x := c.do("POST", "/api/import/eml", "hola", withContentType("message/rfc822")); x.Code != 400 {
		t.Errorf("basura: %d", x.Code)
	}
	if x := c.do("POST", "/api/import/eml", eml(t, "pichincha-consumo"), withContentType("text/plain")); x.Code != 415 {
		t.Errorf("tipo: %d", x.Code)
	}
	if x := c.do("POST", "/api/import/eml", strings.Repeat("a", 3<<20), withContentType("message/rfc822")); x.Code != 413 {
		t.Errorf("tamaño: %d", x.Code)
	}
	// CSRF también aplica a la subida
	if x := c.do("POST", "/api/import/eml", eml(t, "produbanco-consumo"), withContentType("message/rfc822"), withOrigin("https://evil.example")); x.Code != 403 {
		t.Errorf("csrf: %d", x.Code)
	}
}

func TestMismoCorreoEnDosCuentas(t *testing.T) {
	e := newEnv(t)
	a := e.signup("ana@x.com")
	b := e.signup("beto@x.com")
	if a.importEML(t, "diners-compra-columnas").str("status") != "created" || b.importEML(t, "diners-compra-columnas").str("status") != "created" {
		t.Fatal("la deduplicación es por usuario: el mismo correo puede existir en dos cuentas")
	}
	if a.must(200, "GET", "/api/transactions", nil).Body["total"].(float64) != 1 {
		t.Fatal("aislamiento")
	}
}

func TestReglasDeCategoria(t *testing.T) {
	e := newEnv(t)
	c := e.signup("ana@x.com")
	r := c.importEML(t, "produbanco-consumo") // "Pedidosya Groceries" → Comida (diccionario)
	tx := r.Body["transaction"].(map[string]any)
	compras := c.categoryID("Compras")

	// Al corregir la categoría, se aprende la regla
	c.must(200, "PATCH", "/api/transactions/"+tx["id"].(string), map[string]any{"category_id": compras, "learn_rule": true})
	rules := c.must(200, "GET", "/api/merchant-rules", nil)
	if len(rules.List) != 1 || rules.List[0].(map[string]any)["pattern"] != "pedidosya groceries" {
		t.Fatalf("regla: %s", rules.Raw)
	}
	// La regla está cifrada en la BD
	var n int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM merchant_rules WHERE CAST(pattern_enc AS TEXT) LIKE '%pedidos%'`).Scan(&n)
	if n != 0 {
		t.Fatal("patrón en texto plano")
	}
	// Otro correo del mismo comercio usa la regla (gana sobre el diccionario)
	mod := strings.ReplaceAll(eml(t, "produbanco-consumo"), "produbanco-consumo@pruebas.pulse", "otro-id@pruebas.pulse")
	x := c.do("POST", "/api/import/eml", mod, withContentType("message/rfc822"))
	if x.str("category") != "Compras" || x.str("category_by") != "rule" {
		t.Fatalf("debió usar la regla: %s", x.Raw)
	}
	// Reglas manuales y aislamiento
	id := c.must(201, "POST", "/api/merchant-rules", map[string]any{"pattern": "DLC* UBER EATS", "category_id": c.categoryID("Ocio")}).str("id")
	c.must(400, "POST", "/api/merchant-rules", map[string]any{"pattern": "ab", "category_id": compras})
	b := e.signup("beto@x.com")
	b.must(404, "DELETE", "/api/merchant-rules/"+id, nil)
	b.must(400, "POST", "/api/merchant-rules", map[string]any{"pattern": "algo", "category_id": compras})
	if len(b.must(200, "GET", "/api/merchant-rules", nil).List) != 0 {
		t.Fatal("B ve reglas de A")
	}
	c.must(204, "DELETE", "/api/merchant-rules/"+id, nil)
	_ = bytes.MinRead
}

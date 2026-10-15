package mailparse

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "regenerar los archivos .golden.json")

var gye, _ = time.LoadLocation("America/Guayaquil")

func load(t *testing.T, name string) *Email {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".eml"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	e, err := ParseEML(f)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return e
}

// Expectativas escritas a mano (no generadas): son la verdad contra la que se mide el parser.
func TestCorreosDeEjemplo(t *testing.T) {
	cases := []struct {
		file, status, merchant, suffix, when, inst string
		cents                                      int64
	}{
		{"pichincha-consumo", StatusExpense, "Carl S Alban Borja", "989", "2026-10-12 18:24", "Banco Pichincha", 1768},
		{"pichincha-consumo-latin1", StatusExpense, "Supermaxi Ceibos", "989", "2026-10-31 23:30", "Banco Pichincha", 123456},
		{"produbanco-consumo", StatusExpense, "Pedidosya Groceries", "7828", "2026-10-08 16:25", "Produbanco", 434},
		{"produbanco-consumo-miles", StatusExpense, "Aerolinea del Ecuador", "7828", "2026-09-01 08:05", "Produbanco", 125000},
		{"produbanco-consumo-mes-dia-15", StatusExpense, "Viaje de Prueba", "4321", "2026-10-15 13:28", "Produbanco", 197},
		{"produbanco-consumo-mes-dia-15-servicio", StatusExpense, "Servicio de Prueba", "4321", "2026-10-15 15:11", "Produbanco", 2000},
		{"diners-compra-columnas", StatusExpense, "Uber Eats", "666", "2026-06-20 14:31", "Titanium (Diners Club)", 812},
		{"diners-compra-filas", StatusExpense, "Spotify", "666", "2026-06-21 09:05", "Titanium (Diners Club)", 599},
		{"pichincha-pago", StatusIgnored, "", "", "", "", 0},
		{"produbanco-transferencia", StatusIgnored, "", "", "", "", 0},
		{"pichincha-plantilla-nueva", StatusUnrecognized, "", "", "", "", 0},
		{"diners-plantilla-nueva", StatusUnrecognized, "", "", "", "", 0},
		{"produbanco-asunto-no-coincide", StatusUnrecognized, "", "", "", "", 0},
		{"desconocido", StatusUnknown, "", "", "", "", 0},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			r := Analyze(load(t, c.file), gye)
			if r.Status != c.status {
				t.Fatalf("estado %q (%s), esperaba %q", r.Status, r.Reason, c.status)
			}
			if c.status != StatusExpense {
				if r.Expense != nil {
					t.Fatal("un correo que no es gasto no debe producir un gasto")
				}
				if r.Reason == "" {
					t.Fatal("debe explicar por qué")
				}
				return
			}
			x := r.Expense
			if x.AmountCents != c.cents || x.Merchant != c.merchant || x.CardSuffix != c.suffix || x.Institution != c.inst || x.Currency != "USD" || x.Kind != "purchase" {
				t.Fatalf("%+v", x)
			}
			if got := x.OccurredAt.In(gye).Format("2006-01-02 15:04"); got != c.when {
				t.Fatalf("fecha %s, esperaba %s (hora de Ecuador)", got, c.when)
			}
			if x.Confidence < 0.9 || x.Parser == "" {
				t.Fatalf("confianza/parser: %+v", x)
			}
		})
	}
}

// Golden: la salida completa de cada correo queda congelada; cualquier cambio de
// comportamiento aparece como diferencia en la revisión (go test -update para aceptarlo).
func TestGolden(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.eml")
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".eml")
		r := Analyze(load(t, name), gye)
		got, _ := json.MarshalIndent(r, "", "  ")
		golden := filepath.Join("testdata", name+".golden.json")
		if *update {
			os.WriteFile(golden, append(got, '\n'), 0o644)
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("falta %s (corre go test -update)", golden)
		}
		if strings.TrimSpace(string(want)) != strings.TrimSpace(string(got)) {
			t.Errorf("%s cambió:\n%s", name, got)
		}
	}
}

func TestMontos(t *testing.T) {
	ok := []struct {
		in  string
		dec byte
		out int64
	}{
		{"$ 17,68", ',', 1768}, {"USD 4.34", '.', 434}, {"8,12", ',', 812}, {"1.234,56", ',', 123456},
		{"1,234.56", '.', 123456}, {"USD 1,234.56", '.', 123456}, {"0,99", ',', 99}, {"$ 17,68", ',', 1768},
		{"1.000.000,00", ',', 100000000}, {"5", ',', 500}, {"5,5", ',', 550}, {"12 345,00", ',', 1234500},
	}
	for _, c := range ok {
		if got, err := ParseAmount(c.in, c.dec); err != nil || got != c.out {
			t.Errorf("ParseAmount(%q, %q) = %d, %v; esperaba %d", c.in, c.dec, got, err, c.out)
		}
	}
	bad := []struct {
		in  string
		dec byte
	}{
		{"", ','}, {"abc", ','}, {"0,00", ','}, {"-5,00", ','}, {"17,689", ','}, {"4.34", ','}, // con coma decimal, "4.34" es ambiguo: se rechaza
		{"1,23.45", '.'}, {"12,34", '.'}, {"1.2.3", ','}, {"1000000,01", ','}, {"9999999999", ','}, {"1e5", '.'}, {"$", ','},
	}
	for _, c := range bad {
		if got, err := ParseAmount(c.in, c.dec); err == nil {
			t.Errorf("ParseAmount(%q, %q) = %d; debió fallar", c.in, c.dec, got)
		}
	}
}

func TestFechas(t *testing.T) {
	ok := map[string]string{
		"2026-10-12 18:24":          "2026-10-12 18:24",
		"2026-06-20 14:31":          "2026-06-20 14:31",
		"2026-06-20T14:31:05":       "2026-06-20 14:31",
		"8/Octubre/2026 16:25":      "2026-10-08 16:25",
		"8/octubre/2026 16:25":      "2026-10-08 16:25",
		"08/OCT/2026 16:25":         "2026-10-08 16:25",
		"1/Setiembre/2026 08:05":    "2026-09-01 08:05",
		"8 de octubre de 2026":      "2026-10-08 00:00",
		"31/12/2026 23:59":          "2026-12-31 23:59",
		"10/15/2026 13:28":          "2026-10-15 13:28",
		"02/29/2028 23:59":          "2028-02-29 23:59",
		"Fecha: 2026-02-28 07:00 h": "2026-02-28 07:00",
	}
	for in, want := range ok {
		got, err := ParseDate(in, gye)
		if err != nil || got.In(gye).Format("2006-01-02 15:04") != want {
			t.Errorf("ParseDate(%q) = %v, %v; esperaba %s", in, got.In(gye), err, want)
		}
	}
	for _, bad := range []string{"", "ayer", "2026-02-30 10:00", "32/Octubre/2026", "8/Octember/2026", "2026-13-01 10:00", "1999-01-01 10:00", "2026-10-12 25:00", "02/29/2026 10:00", "13/15/2026 10:00", "10/32/2026 10:00"} {
		if _, err := ParseDate(bad, gye); err == nil {
			t.Errorf("ParseDate(%q) debió fallar", bad)
		}
	}
}

func TestComerciosYTarjetas(t *testing.T) {
	m := map[string]string{
		"DLC* UBER EATS":               "Uber Eats",
		"PAYPAL *SPOTIFY":              "Spotify",
		"  CARL S   ALBAN BORJA":       "Carl S Alban Borja",
		"KUSHKI*AEROLINEA DEL ECUADOR": "Aerolinea del Ecuador",
		"MI COMISARIATO.":              "Mi Comisariato",
	}
	for in, want := range m {
		if got := CleanMerchant(in); got != want {
			t.Errorf("CleanMerchant(%q) = %q, esperaba %q", in, got, want)
		}
	}
	if MerchantKey("DLC* UBER  EATS") != "uber eats" || MerchantKey("Café Galletti") != "cafe galletti" {
		t.Error("MerchantKey")
	}
	s := map[string]string{"XXX7828": "7828", "Visa Produbanco XXX7828 .": "7828", "Tarjeta terminada en 666": "666", "989": "989", "**** 1234": "1234", "sin tarjeta": "", "12": ""}
	for in, want := range s {
		if got := CardSuffix(in); got != want {
			t.Errorf("CardSuffix(%q) = %q, esperaba %q", in, got, want)
		}
	}
}

func TestHTMLATexto(t *testing.T) {
	got := HTMLToText(`<html><head><style>.x{}</style><script>alert(1)</script></head><body><!-- comentario -->
		<table><tr><td>Valor</td><td>$&nbsp;17,68</td></tr></table><p>Cr&eacute;dito &amp; m&aacute;s</p></body></html>`)
	if got != "Valor\n$ 17,68\nCrédito & más" {
		t.Fatalf("%q", got)
	}
}

func TestPromocionesYPagosNoSonPlantillasDesconocidas(t *testing.T) {
	subjects := []string{
		"🍽️ Disfruta 10% de descuento en Carlo & Carla",
		"Tienes un Crédito para cumplir tus propósitos",
		"Tu próxima aventura empieza con un descuento increíble 🏝",
		"Renueva tu estilo con 20% OFF en tus marcas favoritas",
		"Tienes un crédito preaprobado. Solicítalo hoy",
		"Tus prendas favoritas con 15% OFF en SEVEN SEVEN",
		"Compra desde $50 y recibe un hoodie gratis 🎁",
		"Renueva tu estilo con 15% OFF en OSTU",
		"Tiempo de pagar tus servicios básicos",
		"🎄 La Navidad empieza con 20% OFF en Roland",
		"Agradecemos su pago",
	}
	for _, from := range Senders() {
		for _, subject := range subjects {
			t.Run(from+"/"+subject, func(t *testing.T) {
				r := Analyze(&Email{From: from, Subject: subject, Text: "Contenido comercial o aviso de pago"}, gye)
				if r.Status != StatusIgnored || r.Expense != nil || r.Parser == "" {
					t.Fatalf("debe ignorarse sin crear gastos ni alertas: %+v", r)
				}
			})
		}
	}
}

func TestPublicidadNoOcultaConsumosNiFormatosNuevos(t *testing.T) {
	for _, subject := range []string{"Consumo Tarjeta de Crédito por USD 20.00", "Compra con descuento por USD 20.00", "Compra en LOCAL DESCUENTO por USD 20.00", "Reverso de consumo con descuento", "Nueva notificación de tu tarjeta"} {
		r := Analyze(&Email{From: "bancaenlinea@produbanco.com", Subject: subject, Text: "Consulta tus promociones y descuentos"}, gye)
		if r.Status != StatusUnrecognized {
			t.Fatalf("%q debe seguir pendiente de análisis: %+v", subject, r)
		}
	}

	e := load(t, "produbanco-consumo")
	e.Text += "\nDisfruta 20% de descuento en tus marcas favoritas"
	if r := Analyze(e, gye); r.Status != StatusExpense || r.Expense.AmountCents != 434 {
		t.Fatalf("la publicidad al pie de un consumo no debe ocultarlo: %+v", r)
	}
	if r := Analyze(&Email{From: "promociones@example.com", Subject: "Agradecemos su pago"}, gye); r.Status != StatusUnknown {
		t.Fatalf("no clasificar remitentes ajenos a los bancos: %+v", r)
	}
}

func TestFechaExplicitaNoSeIntercambiaPorElEnvio(t *testing.T) {
	for _, date := range []string{"8/Octubre/2026 16:25", "2026-10-08 16:25"} {
		e := load(t, "produbanco-consumo")
		e.Text = strings.ReplaceAll(e.Text, "8/Octubre/2026 16:25", date)
		e.Date = time.Date(2026, 8, 10, 12, 0, 0, 0, gye)
		r := Analyze(e, gye)
		if r.Status != StatusExpense || r.Expense.OccurredAt.In(gye).Format("2006-01-02") != "2026-10-08" {
			t.Fatalf("no intercambiar día y mes de una fecha explícita: %+v", r)
		}
	}
}

func TestRedaccionParaServiciosExternos(t *testing.T) {
	e := load(t, "diners-compra-columnas")
	red := Redact(e.Text, "TITULAR DE PRUEBA")
	for _, leak := range []string{"TITULAR", "Hola,"} {
		if strings.Contains(red, leak) {
			t.Errorf("queda %q en el texto redactado:\n%s", leak, red)
		}
	}
	red = Redact("Estimado/a\nJUAN PEREZ LOPEZ\nTarjeta 4111 1111 1111 1111 y 4111111111111111 de juan@correo.com, cédula 0912345678. Valor 8,12 el 2026-06-20 14:31 y 8/Octubre/2026 16:25, termina en 666, Supermaxi")
	for _, leak := range []string{"4111", "juan@correo.com", "0912345678", "JUAN"} {
		if strings.Contains(red, leak) {
			t.Errorf("queda %q: %s", leak, red)
		}
	}
	for _, keep := range []string{"8,12", "2026-06-20 14:31", "8/Octubre/2026 16:25", "666", "Supermaxi"} {
		if !strings.Contains(red, keep) {
			t.Errorf("se perdió %q, necesario para leer el gasto: %s", keep, red)
		}
	}
}

func TestCorreoInvalidoOGigante(t *testing.T) {
	if _, err := ParseEML(strings.NewReader("esto no es un correo")); err == nil {
		t.Error("texto basura aceptado")
	}
	big := "From: a@b.com\r\nSubject: x\r\n\r\n" + strings.Repeat("a", maxEmail+10)
	if _, err := ParseEML(strings.NewReader(big)); err != ErrTooLarge {
		t.Errorf("correo gigante: %v", err)
	}
	e, err := ParseEML(strings.NewReader("From: =?UTF-8?Q?Produbanco_enl=C3=ADnea?= <BancaEnLinea@Produbanco.com>\r\nSubject: =?UTF-8?B?Q29uc3VtbyBUYXJqZXRh?=\r\n\r\nhola"))
	if err != nil || e.From != "bancaenlinea@produbanco.com" || e.FromName != "Produbanco enlínea" || e.Subject != "Consumo Tarjeta" || e.Text != "hola" {
		t.Fatalf("%+v %v", e, err)
	}
}

func FuzzParseAmount(f *testing.F) {
	for _, s := range []string{"$ 17,68", "USD 4.34", "1.234,56", "1,234.56", "8,12"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, d := range []byte{',', '.'} {
			if c, err := ParseAmount(s, d); err == nil && (c <= 0 || c > maxCents) {
				t.Fatalf("%q → %d fuera de rango", s, c)
			}
		}
	})
}

func FuzzParseDate(f *testing.F) {
	f.Add("8/Octubre/2026 16:25")
	f.Add("2026-10-12 18:24")
	f.Fuzz(func(t *testing.T, s string) {
		if d, err := ParseDate(s, gye); err == nil && (d.Year() < 2000 || d.Year() > 2100) {
			t.Fatalf("%q → %v", s, d)
		}
	})
}

func FuzzEML(f *testing.F) {
	files, _ := filepath.Glob("testdata/*.eml")
	for _, p := range files {
		b, _ := os.ReadFile(p)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		e, err := ParseEML(strings.NewReader(string(b)))
		if err != nil {
			return
		}
		r := Analyze(e, gye) // nunca debe entrar en pánico
		if r.Status == StatusExpense && (r.Expense == nil || r.Expense.AmountCents <= 0 || r.Expense.Merchant == "") {
			t.Fatalf("gasto inválido: %+v", r)
		}
	})
}

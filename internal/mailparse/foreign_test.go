package mailparse

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Formato anonimizado del correo recibido: HTML en base64 y salto entre divisa y monto.
func foreignEmail(t *testing.T) *Email {
	t.Helper()
	html := `<p>Fecha y Hora: 11/20/2026 18:05</p>
<p>Transacción: <strong>Consumo Tarjeta de Crédito Produbanco</strong></p>
<p>Se registró un consumo con tu Tarjeta de Crédito Visa Produbanco XXX4321 .</p>
<p><strong>Valor:</strong> EUR
200.00<br><strong>Establecimiento:</strong> COMERCIO DE PRUEBA</p>
<p>Esta transacción tiene un cargo por el valor de $1.96 por concepto de cargos en consumos en el exterior</p>`
	raw := "From: bancaenlinea@produbanco.com\r\nDate: Fri, 20 Nov 2026 18:05:38 -0500\r\nSubject: Consumo Tarjeta de Credito por EUR 200.00\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte(html))
	e, err := ParseEML(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestProdubancoForeignCurrency(t *testing.T) {
	e := foreignEmail(t)
	r := Analyze(e, gye)
	if r.Status != StatusExpense {
		t.Fatalf("%+v", r)
	}
	x := r.Expense
	if x.AmountCents != 20000 || x.Currency != "EUR" || x.ForeignFeeCents != 196 || x.Merchant != "Comercio de Prueba" || x.CardSuffix != "4321" || x.OccurredAt.In(gye).Format("2006-01-02 15:04") != "2026-11-20 18:05" {
		t.Fatalf("%+v", x)
	}
	for _, subject := range []string{"Consumo Tarjeta de Credito por EUR 201.00", "Consumo Tarjeta de Credito por USD 200.00"} {
		e.Subject = subject
		if got := Analyze(e, gye); got.Status != StatusUnrecognized {
			t.Fatalf("%s: %+v", subject, got)
		}
	}
	e.Subject = "Consumo Tarjeta de Credito por GBP 200.00"
	e.Text = strings.Replace(e.Text, "EUR 200.00", "GBP 200.00", 1)
	if got := Analyze(e, gye); got.Status != StatusUnrecognized {
		t.Fatalf("GBP: %+v", got)
	}
	if x, _ := FromExtraction(e, "200.00", "Comercio", "4321", "2026-11-20 18:05", gye); x != nil {
		t.Fatal("Gemini no debe tratar GBP como USD")
	}
}

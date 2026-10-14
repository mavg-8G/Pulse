package mailparse

import (
	"strings"
	"time"
)

// Institution devuelve el banco del remitente, o "" si no es un remitente soportado.
func Institution(e *Email) string {
	switch {
	case pichincha{}.Sender(e.From):
		return "Banco Pichincha"
	case produbanco{}.Sender(e.From):
		return "Produbanco"
	case diners{}.Sender(e.From):
		if containsFold(e.Text, "titanium") {
			return "Titanium (Diners Club)"
		}
		return "Diners Club"
	}
	return ""
}

// FromExtraction valida un gasto que extrajo un servicio externo (Gemini) a partir de un
// correo de un banco soportado. Nunca se confía en él a ciegas: mismo normalizador que los
// parsers, fecha cercana a la del correo y confianza baja (siempre queda por revisar).
// amount usa punto decimal ("12.50"); date es "AAAA-MM-DD HH:MM" en hora local.
func FromExtraction(e *Email, amount, merchant, card, date string, loc *time.Location) (*Expense, string) {
	inst := Institution(e)
	if inst == "" {
		return nil, "remitente no soportado"
	}
	if d := onlyDigits(card); card != "" && (len(d) < 3 || len(d) > 4) {
		card = ""
	} else {
		card = d
	}
	x, why := build(inst, strings.TrimSpace(amount), '.', merchant, card, date, loc)
	if x == nil {
		return nil, why
	}
	if !e.Date.IsZero() && (x.OccurredAt.Before(e.Date.Add(-40*24*time.Hour)) || x.OccurredAt.After(e.Date.Add(24*time.Hour))) {
		return nil, "la fecha no coincide con la del correo"
	}
	if len([]rune(x.Merchant)) > 120 {
		return nil, "establecimiento demasiado largo"
	}
	x.Parser, x.Confidence = "gemini", 0.5
	return x, ""
}

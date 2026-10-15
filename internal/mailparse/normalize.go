package mailparse

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ---------- montos ----------

const maxCents = 100_000_000

var ErrAmount = errors.New("monto inválido")

// ParseAmount convierte un monto a centavos según el separador decimal de cada banco
// (',' o '.'). El otro separador solo se admite como separador de miles en grupos de 3.
//
//	ParseAmount("$ 17,68", ',') = 1768     ParseAmount("USD 4.34", '.') = 434
//	ParseAmount("1.234,56", ',') = 123456  ParseAmount("1,234.56", '.') = 123456
func ParseAmount(s string, decimal byte) (int64, error) {
	s = strings.TrimSpace(s)
	for _, p := range []string{"USD", "US$", "$", "usd"} {
		s = strings.TrimSpace(strings.TrimPrefix(s, p))
	}
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, s)
	if s == "" || len(s) > 20 {
		return 0, ErrAmount
	}
	thousands := byte('.')
	if decimal == '.' {
		thousands = ','
	}
	intPart, frac := s, ""
	if i := strings.LastIndexByte(s, decimal); i >= 0 {
		intPart, frac = s[:i], s[i+1:]
		if len(frac) == 0 || len(frac) > 2 || strings.IndexByte(intPart, decimal) >= 0 {
			return 0, ErrAmount
		}
	}
	groups := strings.Split(intPart, string(thousands))
	for i, g := range groups {
		if g == "" || (i > 0 && len(g) != 3) || (i == 0 && len(groups) > 1 && len(g) > 3) {
			return 0, ErrAmount
		}
	}
	digits := strings.Join(groups, "")
	for _, r := range digits + frac {
		if r < '0' || r > '9' {
			return 0, ErrAmount
		}
	}
	if len(digits) > 9 {
		return 0, ErrAmount
	}
	n, _ := strconv.ParseInt(digits, 10, 64)
	f := int64(0)
	if frac != "" {
		f, _ = strconv.ParseInt((frac + "0")[:2], 10, 64)
	}
	c := n*100 + f
	if c <= 0 || c > maxCents {
		return 0, ErrAmount
	}
	return c, nil
}

// ---------- fechas ----------

var months = map[string]time.Month{
	"enero": 1, "ene": 1, "febrero": 2, "feb": 2, "marzo": 3, "mar": 3, "abril": 4, "abr": 4,
	"mayo": 5, "may": 5, "junio": 6, "jun": 6, "julio": 7, "jul": 7, "agosto": 8, "ago": 8,
	"septiembre": 9, "setiembre": 9, "sep": 9, "sept": 9, "set": 9, "octubre": 10, "oct": 10,
	"noviembre": 11, "nov": 11, "diciembre": 12, "dic": 12,
}

var (
	reISO     = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})\s*(?:T|\s)?\s*(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	reSpanish = regexp.MustCompile(`(\d{1,2})\s*[/\- ]\s*(?:de\s+)?([A-Za-zÁÉÍÓÚáéíóú]+)\.?\s*[/\- ]\s*(?:de\s+)?(\d{4})(?:[,\s]+(?:a las\s+)?(\d{1,2}):(\d{2}))?`)
	reDMY     = regexp.MustCompile(`(\d{1,2})/(\d{1,2})/(\d{4})(?:\s+(\d{1,2}):(\d{2}))?`)
)

var ErrDate = errors.New("fecha inválida")

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func mkDate(y int, m time.Month, d, hh, mm int, loc *time.Location) (time.Time, error) {
	if y < 2000 || y > 2100 || m < 1 || m > 12 || d < 1 || d > 31 || hh > 23 || mm > 59 {
		return time.Time{}, ErrDate
	}
	t := time.Date(y, m, d, hh, mm, 0, 0, loc)
	if t.Day() != d { // 31 de febrero, etc.
		return time.Time{}, ErrDate
	}
	return t.UTC(), nil
}

// ParseDate entiende "2026-10-12 18:24", "8/Octubre/2026 16:25", "8 de octubre de 2026"
// y "08/10/2026 16:25" (día/mes/año). Si el segundo número es mayor que 12,
// admite mes/día/año ("10/15/2026"). La hora es la local del banco (Ecuador).
func ParseDate(s string, loc *time.Location) (time.Time, error) {
	if m := reISO.FindStringSubmatch(s); m != nil {
		return mkDate(atoi(m[1]), time.Month(atoi(m[2])), atoi(m[3]), atoi(m[4]), atoi(m[5]), loc)
	}
	if m := reSpanish.FindStringSubmatch(s); m != nil {
		mon, ok := months[Fold(m[2])]
		if !ok {
			return time.Time{}, fmt.Errorf("%w: mes %q", ErrDate, m[2])
		}
		return mkDate(atoi(m[3]), mon, atoi(m[1]), atoi(m[4]), atoi(m[5]), loc)
	}
	if m := reDMY.FindStringSubmatch(s); m != nil {
		day, month := atoi(m[1]), atoi(m[2])
		if month > 12 && day <= 12 {
			day, month = month, day
		}
		return mkDate(atoi(m[3]), time.Month(month), day, atoi(m[4]), atoi(m[5]), loc)
	}
	return time.Time{}, ErrDate
}

// ---------- comercios ----------

// Prefijos de pasarelas de pago que no forman parte del nombre del comercio.
var gatewayPrefix = regexp.MustCompile(`(?i)^(?:DLC|DLO|DL|PAYPAL|PP|SQ|MP|MERCPAGO|PAYU|KUSHKI|PAYPHONE|DATAFAST|STRIPE|GOOGLE|APPLE\.COM/BILL)\s*\*\s*`)

var lowerWords = map[string]bool{"de": true, "del": true, "la": true, "las": true, "el": true, "los": true, "y": true, "en": true}

// CleanMerchant: "DLC* UBER EATS" → "Uber Eats", "PEDIDOSYA GROCERIES" → "Pedidosya Groceries".
func CleanMerchant(s string) string {
	s = strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
	for i := 0; i < 2; i++ {
		s = strings.TrimSpace(gatewayPrefix.ReplaceAllString(s, ""))
	}
	s = strings.Trim(s, " .,;:-*")
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if i > 0 && lowerWords[w] {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	out := strings.Join(words, " ")
	if len([]rune(out)) > 120 {
		out = string([]rune(out)[:120])
	}
	return out
}

// MerchantKey es la forma comparable de un comercio para las reglas de categoría.
func MerchantKey(s string) string {
	var b strings.Builder
	for _, r := range Fold(CleanMerchant(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") {
			b.WriteByte(' ')
		}
	}
	return strings.TrimSpace(b.String())
}

// ---------- tarjetas ----------

var reSuffix = regexp.MustCompile(`(?i)(?:x{2,}|\*{2,}|terminada en|tarjeta usada|n[uú]mero)\s*:?\s*(\d{3,4})\b|^\s*(\d{3,4})\s*\.?\s*$`)

// CardSuffix extrae los 3–4 últimos dígitos ("XXX7828", "Tarjeta terminada en 666", "989").
func CardSuffix(s string) string {
	m := reSuffix.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// ---------- redacción para servicios externos ----------

var (
	reLongDigits = regexp.MustCompile(`\b\d{6,}\b|\b\d{4}(?:[ -]\d{4}){2,3}\b`) // cédulas, cuentas, tarjetas completas
	reEmail      = regexp.MustCompile(`[\w.+-]+@[\w-]+(\.[\w-]+)+`)
	reGreeting   = regexp.MustCompile(`(?i)^(¡?hola\b|estimad[oa])`)
	reUpperName  = regexp.MustCompile(`^[A-ZÁÉÍÓÚÑ]+(?: [A-ZÁÉÍÓÚÑ]+){1,5}!?$`)
)

// GreetingNames devuelve los nombres que aparecen tras un saludo ("Hola,\nNOMBRE APELLIDO"):
// sirven para quitarlos también del asunto y de otras partes del correo.
func GreetingNames(text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	for i := 0; i+1 < len(lines); i++ {
		if reGreeting.MatchString(strings.TrimSpace(lines[i])) {
			if n := strings.TrimSuffix(strings.TrimSpace(lines[i+1]), "!"); reUpperName.MatchString(n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// Redact quita lo que identifica a la persona antes de enviar un correo a un servicio
// externo (Gemini): nombres del titular, saludos, emails y secuencias de 6+ dígitos.
func Redact(text string, names ...string) string {
	// Saludos ("¡Hola, NOMBRE!", "Estimado/a") y la línea en mayúsculas que los sigue (el nombre).
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if reGreeting.MatchString(strings.TrimSpace(lines[i])) {
			lines[i] = "[SALUDO]"
			if i+1 < len(lines) && reUpperName.MatchString(strings.TrimSpace(lines[i+1])) {
				lines[i+1] = "[TITULAR]"
				i++
			}
		}
	}
	out := strings.Join(lines, "\n")
	for _, n := range names {
		n = strings.TrimSpace(n)
		if len(n) < 3 {
			continue
		}
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(n))
		out = re.ReplaceAllString(out, "[TITULAR]")
	}
	out = reEmail.ReplaceAllString(out, "[EMAIL]")
	out = reLongDigits.ReplaceAllString(out, "[NÚMERO]")
	return out
}

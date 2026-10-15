package mailparse

import (
	"fmt"
	"strings"
	"time"
)

// Expense es lo que se extrae de una notificación de consumo.
type Expense struct {
	Institution string    `json:"institution"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	MerchantRaw string    `json:"merchant_raw"`
	Merchant    string    `json:"merchant"`
	CardSuffix  string    `json:"card_suffix"`
	CardType    string    `json:"card_type"` // credit | debit
	OccurredAt  time.Time `json:"occurred_at"`
	Kind        string    `json:"kind"` // purchase | refund
	Parser      string    `json:"parser"`
	Confidence  float64   `json:"confidence"`
}

// Status del análisis de un correo.
const (
	StatusExpense      = "expense"      // gasto reconocido
	StatusIgnored      = "ignored"      // del banco, pero no es un gasto (pago, aviso, publicidad)
	StatusUnrecognized = "unrecognized" // de un banco conocido con plantilla desconocida → revisar
	StatusUnknown      = "unknown"      // remitente que no es un banco soportado
)

type Result struct {
	Status  string   `json:"status"`
	Parser  string   `json:"parser,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Expense *Expense `json:"expense,omitempty"`
}

type Parser interface {
	Name() string
	// Sender indica si el remitente es de este banco.
	Sender(from string) bool
	Parse(e *Email, loc *time.Location) Result
}

// Parsers registrados. Cada banco nuevo se agrega aquí con sus fixtures en testdata/.
var Parsers = []Parser{pichincha{}, produbanco{}, diners{}}

// Senders devuelve los remitentes conocidos (para la consulta de Gmail en la fase 7).
func Senders() []string {
	return []string{"servicios@tarjetasbancopichincha.com", "bancaenlinea@produbanco.com", "servicios@titanium.com.ec"}
}

// Analyze clasifica un correo con el parser del remitente.
func Analyze(e *Email, loc *time.Location) Result {
	for _, p := range Parsers {
		if p.Sender(e.From) {
			r := p.Parse(e, loc)
			r.Parser = p.Name()
			if r.Expense != nil {
				r.Expense.Parser = p.Name()
			}
			return r
		}
	}
	return Result{Status: StatusUnknown, Reason: "remitente no soportado: " + e.From}
}

func domainOf(from string) string {
	_, d, _ := strings.Cut(strings.ToLower(from), "@")
	return d
}

func fromDomain(from string, domains ...string) bool {
	d := domainOf(from)
	for _, x := range domains {
		if d == x || strings.HasSuffix(d, "."+x) {
			return true
		}
	}
	return false
}

// notExpense detecta correos del banco que no son consumos.
func notExpense(text, subject string) (string, bool) {
	all := subject + "\n" + text
	switch {
	case containsFold(all, "pago recibido", "pago de tu tarjeta", "pago a tu tarjeta", "hemos recibido tu pago", "pago realizado a la tarjeta"):
		return "pago de la tarjeta", true
	case containsFold(all, "codigo de seguridad", "código de seguridad", "clave temporal", "otp", "inicio de sesion", "inicio de sesión", "cambio de clave"):
		return "aviso de seguridad", true
	case containsFold(all, "estado de cuenta"):
		return "estado de cuenta", true
	case containsFold(all, "transferencia"):
		return "transferencia (aún no soportada)", true
	case containsFold(all, "avance de efectivo"):
		return "avance de efectivo (requiere revisión manual)", true
	}
	return "", false
}

func build(inst, rawAmount string, decimal byte, rawMerchant, rawCard, rawDate string, loc *time.Location) (*Expense, string) {
	amt, err := ParseAmount(rawAmount, decimal)
	if err != nil {
		return nil, fmt.Sprintf("monto ilegible %q", rawAmount)
	}
	when, err := ParseDate(rawDate, loc)
	if err != nil {
		return nil, fmt.Sprintf("fecha ilegible %q", rawDate)
	}
	merchant := CleanMerchant(rawMerchant)
	if merchant == "" {
		return nil, "sin establecimiento"
	}
	e := &Expense{Institution: inst, AmountCents: amt, Currency: "USD", MerchantRaw: strings.TrimSpace(rawMerchant), Merchant: merchant,
		CardSuffix: CardSuffix(rawCard), CardType: "credit", OccurredAt: when, Kind: "purchase", Confidence: 0.95}
	if e.CardSuffix == "" {
		e.Confidence = 0.8
	}
	return e, ""
}

// ---------------- Banco Pichincha ----------------
// Remitente: servicios@tarjetasbancopichincha.com — "Has realizado un consumo".
// Valor "$ 17,68" (coma decimal) · Tarjeta usada "989" · Fecha "2026-10-12 18:24".

type pichincha struct{}

func (pichincha) Name() string { return "pichincha-consumo-v1" }

func (pichincha) Sender(from string) bool {
	return fromDomain(from, "tarjetasbancopichincha.com", "pichincha.com")
}

func (pichincha) Parse(e *Email, loc *time.Location) Result {
	if !containsFold(e.Text, "has realizado un consumo") {
		if why, ok := notExpense(e.Text, e.Subject); ok {
			return Result{Status: StatusIgnored, Reason: why}
		}
		return Result{Status: StatusUnrecognized, Reason: "plantilla de Banco Pichincha desconocida"}
	}
	f := fields(e.Text, "Valor", "Establecimiento", "Tarjeta usada", "Fecha")
	exp, why := build("Banco Pichincha", f["Valor"], ',', f["Establecimiento"], f["Tarjeta usada"], f["Fecha"], loc)
	if exp == nil {
		return Result{Status: StatusUnrecognized, Reason: why}
	}
	if containsFold(e.Text, "tarjeta de debito", "tarjeta de débito") {
		exp.CardType = "debit"
	}
	return Result{Status: StatusExpense, Expense: exp}
}

// ---------------- Produbanco ----------------
// Remitente: bancaenlinea@produbanco.com — asunto "Consumo Tarjeta de Crédito por USD 4.34".
// "Fecha y Hora: 8/Octubre/2026 16:25" · "Valor: USD 4.34" (punto decimal) · "Visa Produbanco XXX7828".

type produbanco struct{}

func (produbanco) Name() string { return "produbanco-consumo-v1" }

func (produbanco) Sender(from string) bool { return fromDomain(from, "produbanco.com") }

func (produbanco) Parse(e *Email, loc *time.Location) Result {
	f := fields(e.Text, "Fecha y Hora", "Transacción", "Valor", "Establecimiento")
	isConsumo := strings.HasPrefix(Fold(e.Subject), Fold("Consumo Tarjeta")) || strings.HasPrefix(Fold(f["Transacción"]), "consumo")
	if !isConsumo {
		if why, ok := notExpense(e.Text, e.Subject); ok {
			return Result{Status: StatusIgnored, Reason: why}
		}
		if t := f["Transacción"]; t != "" {
			return Result{Status: StatusIgnored, Reason: "tipo de transacción no soportado: " + t}
		}
		return Result{Status: StatusUnrecognized, Reason: "plantilla de Produbanco desconocida"}
	}
	card := ""
	for _, line := range strings.Split(e.Text, "\n") {
		if containsFold(line, "tarjeta de credito", "tarjeta de crédito", "tarjeta de debito", "tarjeta de débito") && CardSuffix(line) != "" {
			card = line
			break
		}
	}
	exp, why := build("Produbanco", f["Valor"], '.', f["Establecimiento"], card, f["Fecha y Hora"], loc)
	if exp == nil {
		return Result{Status: StatusUnrecognized, Reason: why}
	}
	if containsFold(f["Transacción"]+e.Subject, "debito", "débito") {
		exp.CardType = "debit"
	}
	// Control cruzado: el monto del asunto debe coincidir con el del cuerpo.
	// (se trabaja sobre el asunto ya normalizado: quitar tildes cambia la longitud en bytes)
	if fs := Fold(e.Subject); strings.Contains(fs, "por usd") {
		_, rest, _ := strings.Cut(fs, "por usd")
		if subj, err := ParseAmount(rest, '.'); err == nil && subj != exp.AmountCents {
			return Result{Status: StatusUnrecognized, Reason: fmt.Sprintf("el asunto dice %d centavos y el cuerpo %d", subj, exp.AmountCents)}
		}
	}
	return Result{Status: StatusExpense, Expense: exp}
}

// ---------------- Diners Club / Titanium ----------------
// Remitente: servicios@titanium.com.ec — "Realizaste una compra con tu TITANIUM Visa".
// "Tarjeta terminada en 666" · Fecha "2026-06-20 14:31" · Valor "8,12" (coma, sin símbolo).
// La tabla puede venir con etiqueta y valor por columna o en filas separadas.

type diners struct{}

func (diners) Name() string { return "diners-titanium-compra-v1" }

func (diners) Sender(from string) bool {
	return fromDomain(from, "titanium.com.ec", "dinersclub.com.ec", "interdin.com.ec")
}

func (diners) Parse(e *Email, loc *time.Location) Result {
	if !containsFold(e.Text, "realizaste una compra", "realizaste un consumo") {
		if why, ok := notExpense(e.Text, e.Subject); ok {
			return Result{Status: StatusIgnored, Reason: why}
		}
		return Result{Status: StatusUnrecognized, Reason: "plantilla de Diners/Titanium desconocida"}
	}
	f := fields(e.Text, "Fecha", "Establecimiento", "Valor")
	date := f["Fecha"]
	card := ""
	for _, line := range strings.Split(e.Text, "\n") {
		if containsFold(line, "terminada en") {
			card = line
			break
		}
	}
	inst := "Diners Club"
	if containsFold(e.Text, "titanium") {
		inst = "Titanium (Diners Club)"
	}
	exp, why := build(inst, f["Valor"], ',', f["Establecimiento"], card, date, loc)
	if exp == nil {
		return Result{Status: StatusUnrecognized, Reason: why}
	}
	return Result{Status: StatusExpense, Expense: exp}
}

// Bank devuelve el nombre del banco para un nombre de parser ("pichincha-consumo-v1" → "Banco Pichincha").
func Bank(parser string) string {
	switch {
	case strings.HasPrefix(parser, "pichincha"):
		return "Banco Pichincha"
	case strings.HasPrefix(parser, "produbanco"):
		return "Produbanco"
	case strings.HasPrefix(parser, "diners"):
		return "Diners/Titanium"
	case parser == "gemini":
		return "Gemini"
	}
	return parser
}

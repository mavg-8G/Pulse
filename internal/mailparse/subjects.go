package mailparse

import (
	"regexp"
	"strings"
)

var (
	transactionSubject = regexp.MustCompile(`\b(consumo|compra|compraste|cargo|debito|reverso|devolucion|reembolso)\b`)
	offerPercent       = regexp.MustCompile(`\b\d+(?:[.,]\d+)?\s*%\s*off\b`)
)

// IgnoredSubject recognizes explicit offers and card-payment acknowledgements.
// It is also used to exclude old false alarms. Transaction subjects take priority:
// an unreadable purchase must stay visible, even if it mentions a discount.
func IgnoredSubject(subject string) (string, bool) {
	s := strings.Join(strings.Fields(Fold(subject)), " ")
	if strings.HasPrefix(s, "compra desde") && strings.Contains(s, "recibe") && strings.Contains(s, "gratis") &&
		!transactionSubject.MatchString(strings.TrimPrefix(s, "compra desde")) {
		return "publicidad del banco", true
	}
	if transactionSubject.MatchString(s) {
		return "", false
	}
	if containsFold(s, "agradecemos su pago", "agradecemos tu pago", "hemos recibido tu pago", "pago recibido", "pago de tu tarjeta", "pago a tu tarjeta") {
		return "pago de la tarjeta", true
	}
	if containsFold(s, "descuento", "credito preaprobado", "tienes un credito para", "tiempo de pagar tus servicios basicos") ||
		offerPercent.MatchString(s) {
		return "publicidad del banco", true
	}
	return "", false
}

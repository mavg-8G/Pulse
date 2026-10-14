package mailparse

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

var (
	reDropBlocks = regexp.MustCompile(`(?is)<(script|style|head|title|noscript)\b.*?</(script|style|head|title|noscript)\s*>|<!--.*?-->`)
	reBreakTags  = regexp.MustCompile(`(?i)</?(br|p|div|tr|td|th|li|ul|ol|table|tbody|thead|h[1-6]|section|article|header|footer|center|blockquote)\b[^>]*>`)
	reAnyTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces     = regexp.MustCompile(`[ \t\x{00A0}\x{200B}\x{FEFF}]+`)
)

// HTMLToText convierte HTML en texto: cada celda, párrafo o salto queda en su propia
// línea, así "Valor" y "$ 17,68" en celdas distintas se leen como etiqueta y valor.
func HTMLToText(s string) string {
	s = reDropBlocks.ReplaceAllString(s, " ")
	s = reBreakTags.ReplaceAllString(s, "\n")
	s = reAnyTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return normalizeText(s)
}

var reDateLine = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$|^\d{1,2}/\d{1,2}/\d{4}$`)
var reTimeLine = regexp.MustCompile(`^\d{1,2}:\d{2}(:\d{2})?$`)

// mergeDateTime une "2026-06-20" y "14:31" cuando la hora quedó en la línea siguiente
// (celdas con <br>), para que las columnas de una tabla no se desalineen.
func mergeDateTime(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if reDateLine.MatchString(lines[i]) && i+1 < len(lines) && reTimeLine.MatchString(lines[i+1]) {
			out = append(out, lines[i]+" "+lines[i+1])
			i++
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(reSpaces.ReplaceAllString(line, " "))
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// Fold quita tildes y pasa a minúsculas para comparar frases ("CRÉDITO" == "credito").
func Fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'ä', 'â':
			r = 'a'
		case 'é', 'è', 'ë', 'ê':
			r = 'e'
		case 'í', 'ì', 'ï', 'î':
			r = 'i'
		case 'ó', 'ò', 'ö', 'ô':
			r = 'o'
		case 'ú', 'ù', 'ü', 'û':
			r = 'u'
		case 'ñ':
			r = 'n'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// fields busca etiquetas y sus valores en dos disposiciones comunes de tablas HTML:
//
//	vertical:   Valor / $ 17,68 / Establecimiento / CARL S…   (etiqueta y valor seguidos)
//	horizontal: Fecha / Establecimiento / Valor / 2026-06-20 14:31 / DLC* UBER EATS / 8,12
//
// También acepta "Etiqueta: valor" en la misma línea.
func fields(text string, labels ...string) map[string]string {
	lines := strings.Split(mergeDateTime(text), "\n")
	isLabel := func(l string) (string, bool) {
		f := strings.TrimSuffix(strings.TrimSpace(Fold(l)), ":")
		for _, lb := range labels {
			if f == Fold(lb) {
				return lb, true
			}
		}
		return "", false
	}
	out := map[string]string{}
	for i := 0; i < len(lines); i++ {
		// "Etiqueta: valor"
		if k, v, ok := strings.Cut(lines[i], ":"); ok {
			if lb, ok := isLabel(k); ok && strings.TrimSpace(v) != "" {
				if _, seen := out[lb]; !seen {
					out[lb] = strings.TrimSpace(v)
				}
				continue
			}
		}
		lb, ok := isLabel(lines[i])
		if !ok {
			continue
		}
		// fila de encabezados: varias etiquetas seguidas
		run := []string{lb}
		j := i + 1
		for ; j < len(lines); j++ {
			next, ok := isLabel(lines[j])
			if !ok {
				break
			}
			run = append(run, next)
		}
		for k, name := range run {
			if j+k < len(lines) {
				if _, seen := out[name]; !seen {
					out[name] = lines[j+k]
				}
			}
		}
		i = j + len(run) - 1
	}
	return out
}

func containsFold(text string, phrases ...string) bool {
	f := Fold(text)
	for _, p := range phrases {
		if strings.Contains(f, Fold(p)) {
			return true
		}
	}
	return false
}

func onlyDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

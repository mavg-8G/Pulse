// Package budget contiene los cálculos puros del plan de gastos: rangos de mes en la
// zona horaria del usuario, progreso, proyección y umbrales.
package budget

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrMonth = errors.New("mes inválido (formato AAAA-MM)")

// MonthRange devuelve [inicio, fin) del mes en UTC, calculado en la zona del usuario.
func MonthRange(month string, loc *time.Location) (time.Time, time.Time, error) {
	t, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil || t.Year() < 2000 || t.Year() > 2200 {
		return time.Time{}, time.Time{}, ErrMonth
	}
	return t.UTC(), t.AddDate(0, 1, 0).UTC(), nil
}

// MonthOf devuelve "AAAA-MM" del instante en la zona del usuario.
func MonthOf(t time.Time, loc *time.Location) string { return t.In(loc).Format("2006-01") }

func DaysIn(month string, loc *time.Location) int {
	s, e, err := MonthRange(month, loc)
	if err != nil {
		return 0
	}
	return int(e.In(loc).Sub(s.In(loc)).Hours()+12) / 24
}

type Progress struct {
	BudgetCents        int64  `json:"budget_cents"`
	SpentCents         int64  `json:"spent_cents"`
	RemainingCents     int64  `json:"remaining_cents"`
	Percent            int    `json:"percent"`
	ProjectedCents     int64  `json:"projected_cents"`
	ProjectedOverCents int64  `json:"projected_over_cents"`
	DaysElapsed        int    `json:"days_elapsed"`
	DaysInMonth        int    `json:"days_in_month"`
	Status             string `json:"status"` // ok | warning | over
}

// Compute calcula el progreso de un presupuesto en un mes dado y "ahora".
// La proyección extrapola el ritmo diario al mes completo; en meses pasados o
// futuros la proyección es lo gastado.
func Compute(budgetCents, spentCents int64, month string, now time.Time, loc *time.Location) Progress {
	p := Progress{BudgetCents: budgetCents, SpentCents: spentCents, DaysInMonth: DaysIn(month, loc)}
	p.RemainingCents = budgetCents - spentCents
	switch {
	case budgetCents > 0:
		p.Percent = int(spentCents * 100 / budgetCents)
	case spentCents > budgetCents:
		// El exceso arrastrado del mes anterior dejó el tope en cero o menos: el mes empieza agotado.
		p.Percent = 100
	}
	start, end, err := MonthRange(month, loc)
	switch {
	case err != nil:
		p.ProjectedCents = spentCents
	case now.Before(start):
		p.ProjectedCents = spentCents
	case !now.Before(end):
		p.DaysElapsed = p.DaysInMonth
		p.ProjectedCents = spentCents
	default:
		p.DaysElapsed = now.In(loc).Day() // día 1 → 1: nunca divide entre cero
		if spentCents > 0 {
			// redondeo al centavo más cercano
			p.ProjectedCents = (spentCents*int64(p.DaysInMonth)*2 + int64(p.DaysElapsed)) / (2 * int64(p.DaysElapsed))
		}
		if p.ProjectedCents < spentCents {
			p.ProjectedCents = spentCents
		}
	}
	if over := p.ProjectedCents - budgetCents; over > 0 {
		p.ProjectedOverCents = over
	}
	switch {
	case spentCents > budgetCents:
		p.Status = "over"
	case spentCents*100 >= budgetCents*80 || p.ProjectedOverCents > 0:
		p.Status = "warning"
	default:
		p.Status = "ok"
	}
	return p
}

// ParseThresholds valida "50,80,100": enteros 1–200, sin repetir, ordenados.
func ParseThresholds(s string) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 200 {
			return nil, fmt.Errorf("umbral inválido %q (1–200)", part)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 || len(out) > 5 {
		return nil, errors.New("debe haber entre 1 y 5 umbrales")
	}
	sort.Ints(out)
	return out, nil
}

func FormatThresholds(t []int) string {
	parts := make([]string, len(t))
	for i, v := range t {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// Crossed devuelve los umbrales alcanzados (gasto ≥ umbral% del presupuesto). Con un tope de cero o
// negativo (exceso arrastrado) y algo gastado o por cubrir, todos los umbrales están alcanzados.
func Crossed(thresholds []int, budgetCents, spentCents int64) []int {
	var out []int
	for _, t := range thresholds {
		if budgetCents > 0 && spentCents*100 >= int64(t)*budgetCents || budgetCents <= 0 && spentCents > budgetCents {
			out = append(out, t)
		}
	}
	return out
}

// ShiftMonth suma n meses a "AAAA-MM".
func ShiftMonth(month string, n int) (string, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil || t.Year() < 2000 || t.Year() > 2200 {
		return "", ErrMonth
	}
	return t.AddDate(0, n, 0).Format("2006-01"), nil
}

// MaxCarryMonths limita cuántos meses hacia atrás se encadena el arrastre.
const MaxCarryMonths = 36

// Plan describe un presupuesto a lo largo de los meses: un tope general, topes de meses concretos
// y, si Rollover, el sobrante (o exceso) de cada mes desde RolloverFrom pasa al siguiente.
type Plan struct {
	BaseCents    int64
	Months       map[string]int64
	Rollover     bool
	RolloverFrom string // AAAA-MM
}

// CapFor es el tope del mes antes de sumar el arrastre.
func (p Plan) CapFor(month string) int64 {
	if v, ok := p.Months[month]; ok {
		return v
	}
	return p.BaseCents
}

// Carry devuelve lo que llega a month desde los meses anteriores: positivo si sobró, negativo si se excedió.
// spent(m) es lo gastado en m (compras menos reversos, con subcategorías). Los meses anteriores a
// RolloverFrom no cuentan, y como mucho se encadenan MaxCarryMonths.
func (p Plan) Carry(month string, spent func(string) (int64, error)) (int64, error) {
	if !p.Rollover || p.RolloverFrom == "" || month <= p.RolloverFrom {
		return 0, nil
	}
	start, err := ShiftMonth(month, -MaxCarryMonths)
	if err != nil {
		return 0, err
	}
	if p.RolloverFrom > start {
		start = p.RolloverFrom
	}
	var carry int64
	for m := start; m < month; {
		v, err := spent(m)
		if err != nil {
			return 0, err
		}
		carry += p.CapFor(m) - v
		if m, err = ShiftMonth(m, 1); err != nil {
			return 0, err
		}
	}
	return carry, nil
}

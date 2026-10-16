package api

import (
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"pulse/internal/store"
)

// Filtros, periodo anterior equivalente, serie de evolución y comercios de Reportes.

// reportFilter limita el reporte a una categoría principal (con sus subcategorías) y/o a una cuenta principal (con
// las cuentas fusionadas en ella). Vacío: sin filtro.
type reportFilter struct {
	Category string `json:"category"`
	Account  string `json:"account"`
}

func (f reportFilter) match(rootCategory, mainAccount string) bool {
	return (f.Category == "" || rootCategory == f.Category) && (f.Account == "" || mainAccount == f.Account)
}

type reportOptions struct {
	Filter  reportFilter
	Compare string    // "previous" (mismo largo justo antes) o "year" (mismas fechas del año anterior)
	First   time.Time // primer movimiento del usuario (cero: ninguno), para saber si el periodo anterior es comparable
}

// parseReportFilter valida los filtros contra los datos del usuario: nunca se filtra por un id ajeno.
func (s *Server) parseReportFilter(r *http.Request, id *identity, cats []store.Category) (reportFilter, error) {
	q := r.URL.Query()
	f := reportFilter{Category: q.Get("category"), Account: q.Get("account")}
	if f.Category != "" {
		ok := false
		for _, c := range cats {
			if c.ID == f.Category && c.ParentID == "" {
				ok = true
			}
		}
		if !ok {
			return f, badRequest("La categoría no existe.")
		}
	}
	if f.Account != "" {
		a, err := s.store.Account(r.Context(), id.user.ID, f.Account)
		if err != nil {
			return f, badRequest("La cuenta no existe.")
		}
		if a.MergedInto != "" { // una cuenta fusionada se reporta con su principal
			f.Account = a.MergedInto
		}
	}
	return f, nil
}

func compareParam(r *http.Request) (string, error) {
	switch v := r.URL.Query().Get("compare"); v {
	case "", "previous":
		return "previous", nil
	case "year":
		return "year", nil
	default:
		return "", badRequest("Comparación inválida.")
	}
}

// shiftMonths mueve una fecha local n meses conservando el día; si ese día no existe, usa el último del mes.
func shiftMonths(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(t.Day(), last), 0, 0, 0, 0, t.Location())
}

func localDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// reportPeriod: fechas locales del periodo transcurrido (to incluido).
type reportPeriod struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Days        int    `json:"days"`
	Compare     string `json:"compare"`
	Granularity string `json:"granularity"` // day, week o month: cómo se agrupa la serie
}

// reportPrevious resume el periodo equivalente anterior. Coverage dice si había datos desde su inicio
// ("completo"), solo desde la mitad ("parcial") o ninguno ("sin_datos"): los porcentajes solo se comparan si es
// completo.
type reportPrevious struct {
	From          string          `json:"from"`
	To            string          `json:"to"`
	Days          int             `json:"days"`
	Coverage      string          `json:"coverage"`
	TotalCents    int64           `json:"total_cents"`
	PurchaseCents int64           `json:"purchase_cents"`
	RefundCents   int64           `json:"refund_cents"`
	PurchaseCount int             `json:"purchase_count"`
	ByCategory    []reportIDTotal `json:"by_category"`
	ByAccount     []reportIDTotal `json:"by_account"`
}

// reportPoint: un punto de la serie de evolución. PreviousCents es el gasto del mismo tramo del periodo anterior
// (sus fechas desplazadas al periodo actual).
type reportPoint struct {
	Start         string `json:"start"`
	End           string `json:"end"`
	NetCents      int64  `json:"net_cents"`
	PurchaseCents int64  `json:"purchase_cents"`
	RefundCents   int64  `json:"refund_cents"`
	PreviousCents int64  `json:"previous_cents"`
}

// reportChange: gasto neto de una categoría contra el periodo anterior. Pct es nil si no es comparable (el periodo
// anterior no tuvo gasto positivo en ella o no está completo).
type reportChange struct {
	ID            string   `json:"id"`
	CurrentCents  int64    `json:"current_cents"`
	PreviousCents int64    `json:"previous_cents"`
	DiffCents     int64    `json:"diff_cents"`
	Pct           *float64 `json:"pct"`
}

type reportMerchant struct {
	Name          string `json:"name"`
	SpentCents    int64  `json:"spent_cents"`
	Count         int    `json:"purchase_count"`
	PreviousCents int64  `json:"previous_cents"`
}

func pctChange(cur, prev int64) *float64 {
	if prev <= 0 {
		return nil
	}
	v := math.Round(float64(cur-prev)/float64(prev)*1000) / 10
	return &v
}

// buckets parte [from, end) en tramos: días (hasta 2 meses), semanas de lunes a domingo (hasta 6) o meses.
func buckets(from, end time.Time, months int) (string, []time.Time) {
	g := "month"
	switch {
	case months <= 2:
		g = "day"
	case months <= 6:
		g = "week"
	}
	var starts []time.Time
	for d := from; d.Before(end) && len(starts) < 1200; {
		starts = append(starts, d)
		switch g {
		case "day":
			d = d.AddDate(0, 0, 1)
		case "week":
			d = d.AddDate(0, 0, 7-(int(d.Weekday())+6)%7) // siguiente lunes
		default:
			d = time.Date(d.Year(), d.Month()+1, 1, 0, 0, 0, 0, d.Location())
		}
	}
	return g, starts
}

// bucketOf devuelve el tramo que contiene la fecha local d (-1 si queda fuera).
func bucketOf(starts []time.Time, end, d time.Time) int {
	if len(starts) == 0 || d.Before(starts[0]) || !d.Before(end) {
		return -1
	}
	return sort.Search(len(starts), func(i int) bool { return starts[i].After(d) }) - 1
}

// merchantKey agrupa variantes del mismo comercio: sin mayúsculas ni espacios repetidos.
func merchantKey(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), " ")
}

// merchantRanking: los comercios con más gasto neto del periodo, con su gasto en el periodo anterior.
func merchantRanking(txs []store.Transaction, loc *time.Location, root func(string) string, f reportFilter, from, end, prevFrom, prevEnd time.Time, limit int) []reportMerchant {
	type agg struct {
		reportMerchant
		seen bool
	}
	m := map[string]*agg{}
	for _, t := range txs {
		key := merchantKey(t.Merchant)
		if key == "" || (f.Category != "" && root(t.CategoryID) != f.Category) {
			continue
		}
		v := t.AmountCents
		if t.Kind == "refund" {
			v = -v
		}
		d := localDate(t.OccurredAt.In(loc))
		a := m[key]
		if a == nil {
			a = &agg{}
			m[key] = a
		}
		switch {
		case !d.Before(from) && d.Before(end):
			if !a.seen { // la lista viene del más reciente al más antiguo: se muestra el nombre más reciente
				a.Name, a.seen = strings.TrimSpace(t.Merchant), true
			}
			a.SpentCents += v
			if t.Kind != "refund" {
				a.Count++
			}
		case !d.Before(prevFrom) && d.Before(prevEnd):
			a.PreviousCents += v
		}
	}
	out := []reportMerchant{}
	for _, a := range m {
		if a.seen && a.SpentCents > 0 {
			out = append(out, a.reportMerchant)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SpentCents != out[j].SpentCents {
			return out[i].SpentCents > out[j].SpentCents
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

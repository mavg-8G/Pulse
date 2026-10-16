// Package forecast proyecta el gasto hasta fin de mes (fase 13).
//
// Separa el gasto en tres partes: los cobros de gastos fijos (recurrentes), los valores atípicos (compras muy por
// encima de lo habitual, que se listan y no se extrapolan) y el gasto variable, que es lo único que modelan los
// métodos. Cada método se evalúa en los meses cerrados como si fuera el mismo día del mes que hoy, usando solo lo
// que se sabía en ese momento, y se elige el de menor error absoluto medio. El método actual ("ritmo": gastado ÷
// días transcurridos × días del mes) entra en esa comparación, así que el elegido nunca tiene más error que él.
//
// Todo es aritmética pura sobre fechas locales: no hay divisiones entre cero (cada método dice si está disponible)
// y los días se cuentan por fecha de calendario, así que los cambios de horario no mueven gastos de día.
package forecast

import (
	"math"
	"math/rand/v2"
	"sort"
	"time"
)

const (
	Ritmo      = "ritmo"       // método actual: gastado ÷ días transcurridos × días del mes
	Regresion  = "regresion"   // regresión lineal sobre el acumulado diario, con intervalo de predicción
	MediaMovil = "media_movil" // media móvil ponderada de los últimos 28 días
	DiaSemana  = "dia_semana"  // promedio de cada día de la semana en las últimas 8 semanas
	Perfil     = "perfil"      // estacionalidad mensual: qué parte del mes suele estar gastada a esta altura

	BacktestMonths   = 6  // meses cerrados con los que se elige el método
	MinRegressionDay = 5  // días del mes necesarios para la regresión
	MinHistoryDays   = 28 // historial para media móvil y día de la semana
	MinProfileMonths = 3  // meses cerrados para el perfil del mes
	MinOutlierSample = 20 // compras necesarias para detectar atípicos
)

// Methods en orden de preferencia ante empates: primero el más simple.
var Methods = []string{Ritmo, Regresion, MediaMovil, DiaSemana, Perfil}

// Tx es un movimiento ya llevado a la fecha local y a su categoría raíz.
type Tx struct {
	Category  string
	Day       time.Time // medianoche local
	Cents     int64     // neto: compras positivas, devoluciones negativas
	Purchase  bool
	Recurring bool // lo generó un gasto fijo
}

// Upcoming es un cobro de gasto fijo que todavía no ocurre este mes.
type Upcoming struct {
	Category string
	Day      time.Time
	Cents    int64
}

type Input struct {
	Today      time.Time // medianoche local de hoy
	FirstDay   time.Time // primer día con movimientos del usuario (cero: ninguno)
	Categories []string  // categorías raíz, en orden de presentación
	Txs        []Tx      // desde WindowStart(Today) hasta hoy
	Upcoming   []Upcoming
}

type Method struct {
	ID             string   `json:"id"`
	Available      bool     `json:"available"`
	ProjectedCents int64    `json:"projected_cents"`
	LowCents       *int64   `json:"low_cents"`
	HighCents      *int64   `json:"high_cents"`
	MAECents       *int64   `json:"mae_cents"` // error absoluto medio en los meses cerrados
	MAPE           *float64 `json:"mape"`      // error porcentual medio (solo meses con gasto)
	Evaluated      int      `json:"evaluated"` // meses cerrados en los que se pudo evaluar
	Candidate      bool     `json:"candidate"` // evaluable en todos los meses: compite en la selección
}

type Category struct {
	ID             string `json:"id"`
	SpentCents     int64  `json:"spent_cents"`
	UpcomingCents  int64  `json:"upcoming_cents"`
	ProjectedCents int64  `json:"projected_cents"`
}

// Accuracy compara, para un mes cerrado, la proyección hecha al mismo día del mes con el cierre real.
type Accuracy struct {
	Month          string   `json:"month"`
	Day            int      `json:"day"`
	ActualCents    int64    `json:"actual_cents"`
	PredictedCents int64    `json:"predicted_cents"`
	BaselineCents  int64    `json:"baseline_cents"`
	ErrorCents     int64    `json:"error_cents"`
	ErrorPct       *float64 `json:"error_pct"`
	BaselinePct    *float64 `json:"baseline_error_pct"`
}

type Outlier struct {
	Date     string `json:"date"`
	Category string `json:"category_id"`
	Cents    int64  `json:"amount_cents"`
}

type Result struct {
	Month          string     `json:"month"`
	Day            int        `json:"day"`
	DaysInMonth    int        `json:"days_in_month"`
	SpentCents     int64      `json:"spent_cents"`
	UpcomingCents  int64      `json:"upcoming_cents"`
	Method         string     `json:"method"`
	ProjectedCents int64      `json:"projected_cents"`
	LowCents       *int64     `json:"low_cents"`
	HighCents      *int64     `json:"high_cents"`
	BaselineCents  int64      `json:"baseline_cents"`
	Methods        []Method   `json:"methods"`
	ByCategory     []Category `json:"by_category"`
	Accuracy       []Accuracy `json:"accuracy"`
	Outliers       []Outlier  `json:"outliers"`
	OutlierAbove   *int64     `json:"outlier_threshold_cents"`
	Warnings       []string   `json:"warnings"`
	// Acumulado diario del mes en curso (hasta hoy) y del mes anterior completo.
	Cumulative         []int64 `json:"cumulative"`
	PreviousCumulative []int64 `json:"previous_cumulative"`
	PreviousMonth      string  `json:"previous_month"`
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// WindowStart es desde cuándo hacen falta movimientos: los meses del backtest y, antes del más antiguo, otros tantos
// para su perfil del mes (que también cubren las 8 semanas de la media móvil y el día de la semana).
func WindowStart(today time.Time) time.Time {
	return monthStart(today).AddDate(0, -2*BacktestMonths, 0)
}

// dayNumber cuenta días de calendario (inmune a cambios de horario).
func dayNumber(t time.Time) int {
	return int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

func daysIn(start time.Time) int { return dayNumber(start.AddDate(0, 1, 0)) - dayNumber(start) }

// tValue: t de Student para un intervalo del 95 % (dos colas).
func tValue(df int) float64 {
	table := [...]float64{12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228, 2.201, 2.179, 2.160, 2.145, 2.131,
		2.120, 2.110, 2.101, 2.093, 2.086, 2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042}
	switch {
	case df < 1:
		return math.Inf(1)
	case df <= len(table):
		return table[df-1]
	default:
		return 1.96 + (2.042-1.96)*30/float64(df)
	}
}

// Regress ajusta y = a + b·x sobre x = 1..n y predice en x = at, con la mitad del intervalo de predicción al 95 %.
// ok es falso con menos de 3 puntos (sin grados de libertad para el error).
func Regress(y []float64, at float64) (pred, half float64, ok bool) {
	n := float64(len(y))
	if len(y) < 3 {
		return 0, 0, false
	}
	mx := (n + 1) / 2
	var my float64
	for _, v := range y {
		my += v
	}
	my /= n
	var sxx, sxy float64
	for i, v := range y {
		dx := float64(i+1) - mx
		sxx += dx * dx
		sxy += dx * (v - my)
	}
	b := sxy / sxx
	a := my - b*mx
	var sse float64
	for i, v := range y {
		r := v - (a + b*float64(i+1))
		sse += r * r
	}
	s := math.Sqrt(sse / (n - 2))
	pred = a + b*at
	half = tValue(len(y)-2) * s * math.Sqrt(1+1/n+(at-mx)*(at-mx)/sxx)
	return pred, half, true
}

// Quartiles con interpolación lineal (tipo 7, el de las hojas de cálculo).
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

// series guarda el gasto diario de una categoría desde el inicio de la ventana.
type series struct {
	variable []float64 // gasto que modelan los métodos
	fixed    []float64 // gastos fijos y atípicos: se suman tal cual, no se extrapolan
	recur    []float64 // solo gastos fijos (para saber qué cobros eran previsibles en un mes cerrado)
}

type engine struct {
	in       Input
	start    time.Time // inicio de la ventana
	base     int       // dayNumber(start)
	first    int       // índice del primer día con datos (puede ser negativo)
	n        int       // días en la ventana hasta hoy, inclusive
	cats     []string
	byCat    map[string]*series
	total    *series
	outliers []Outlier
	thresh   *int64
}

func (e *engine) idx(t time.Time) int { return dayNumber(t) - e.base }

func newEngine(in Input) *engine {
	e := &engine{in: in, start: WindowStart(in.Today), byCat: map[string]*series{}}
	e.base = dayNumber(e.start)
	e.n = e.idx(in.Today) + 1
	e.first = e.n // sin datos: nada disponible
	if !in.FirstDay.IsZero() {
		e.first = e.idx(in.FirstDay)
	}
	seen := map[string]bool{}
	add := func(c string) {
		if !seen[c] {
			seen[c] = true
			e.cats = append(e.cats, c)
			e.byCat[c] = &series{make([]float64, e.n), make([]float64, e.n), make([]float64, e.n)}
		}
	}
	for _, c := range in.Categories {
		add(c)
	}
	e.total = &series{make([]float64, e.n), make([]float64, e.n), make([]float64, e.n)}

	// Atípicos: compras variables por encima de Q3 + 3·IQR en escala logarítmica (los montos de compras son muy
	// asimétricos) y, además, de 10 veces la mediana; así un gasto semanal grande pero habitual no cuenta.
	var amounts []float64
	for _, t := range in.Txs {
		if t.Purchase && !t.Recurring && t.Cents > 0 {
			amounts = append(amounts, math.Log(float64(t.Cents)))
		}
	}
	limit := math.Inf(1)
	if len(amounts) >= MinOutlierSample {
		sort.Float64s(amounts)
		q1, q3 := quantile(amounts, 0.25), quantile(amounts, 0.75)
		limit = math.Max(math.Exp(q3+3*(q3-q1)), 10*math.Exp(quantile(amounts, 0.5)))
		v := int64(math.Floor(limit))
		e.thresh = &v
	}
	for _, t := range in.Txs {
		i := e.idx(t.Day)
		if i < 0 || i >= e.n {
			continue
		}
		add(t.Category)
		s := e.byCat[t.Category]
		v := float64(t.Cents)
		switch {
		case t.Recurring:
			s.fixed[i] += v
			s.recur[i] += v
			e.total.fixed[i] += v
			e.total.recur[i] += v
		case t.Purchase && v > limit:
			s.fixed[i] += v
			e.total.fixed[i] += v
			e.outliers = append(e.outliers, Outlier{Date: t.Day.Format("2006-01-02"), Category: t.Category, Cents: t.Cents})
		default:
			s.variable[i] += v
			e.total.variable[i] += v
		}
	}
	return e
}

func sum(a []float64, from, to int) float64 { // [from, to)
	var s float64
	for i := max(from, 0); i < to && i < len(a); i++ {
		s += a[i]
	}
	return s
}

// month describe un mes evaluado al día k: s es el índice de su día 1.
type month struct {
	start time.Time
	s, k  int
	days  int
}

// remaining predice el gasto variable de los días k+1..fin con un método; solo usa datos hasta el día k.
// También devuelve el acumulado variable al día k, para que el llamador sume.
func (e *engine) remaining(method string, v []float64, m month, profile float64) (float64, bool) {
	end := m.s + m.k // primer índice que NO se conoce
	ck := sum(v, m.s, end)
	left := float64(m.days - m.k)
	switch method {
	case Regresion:
		if m.k < MinRegressionDay {
			return 0, false
		}
		y := make([]float64, m.k)
		acc := 0.0
		for j := 0; j < m.k; j++ {
			acc += v[m.s+j]
			y[j] = acc
		}
		pred, _, ok := Regress(y, float64(m.days))
		return math.Max(0, pred-ck), ok
	case MediaMovil:
		from := max(end-28, e.first)
		if end-from < MinHistoryDays/2 {
			return 0, false
		}
		var num, den float64
		for i := from; i < end; i++ {
			w := float64(i - from + 1)
			num += w * v[i]
			den += w
		}
		return math.Max(0, num/den) * left, true
	case DiaSemana:
		from := max(end-56, e.first)
		if end-from < MinHistoryDays {
			return 0, false
		}
		var sums, counts [7]float64
		for i := from; i < end; i++ {
			wd := e.start.AddDate(0, 0, i).Weekday()
			sums[wd] += v[i]
			counts[wd]++
		}
		r := 0.0
		for j := m.k; j < m.days; j++ {
			wd := m.start.AddDate(0, 0, j).Weekday()
			r += sums[wd] / counts[wd] // con 28 días o más cada día de la semana aparece al menos 4 veces
		}
		return math.Max(0, r), true
	case Perfil:
		if profile <= 0 {
			return 0, false
		}
		return math.Max(0, ck*(1/profile-1)), true
	}
	return 0, false
}

// profile: fracción media del gasto variable total que suele llevarse al día k (proporcional), en los meses cerrados
// anteriores a m. 0 si no hay suficientes meses o si la fracción es tan baja que dispararía la proyección.
func (e *engine) profile(m month) float64 {
	var shares []float64
	for j := 1; j <= BacktestMonths; j++ {
		ps := m.start.AddDate(0, -j, 0)
		if !e.evaluable(ps) {
			break
		}
		s, days := e.idx(ps), daysIn(ps)
		total := sum(e.total.variable, s, s+days)
		if total <= 0 {
			continue
		}
		// Mismo punto proporcional del mes (día 15 de 31 ≈ día 14,5 de 30), interpolando dentro del día.
		pos := float64(m.k) * float64(days) / float64(m.days)
		whole := int(pos)
		at := sum(e.total.variable, s, s+whole)
		if whole < days {
			at += (pos - float64(whole)) * e.total.variable[s+whole]
		}
		shares = append(shares, at/total)
	}
	if len(shares) < MinProfileMonths {
		return 0
	}
	var p float64
	for _, v := range shares {
		p += v
	}
	p /= float64(len(shares))
	if p < 0.05 || p > 1 {
		return 0
	}
	return p
}

// evaluable: el mes empieza dentro de la ventana y el usuario ya tenía datos desde sus primeros días.
func (e *engine) evaluable(ms time.Time) bool {
	s := e.idx(ms)
	return s >= 0 && e.first <= s+2
}

// predict proyecta el total del mes m con un método. upcoming son los cobros fijos aún pendientes por categoría.
func (e *engine) predict(method string, m month, upcoming map[string]float64) (map[string]float64, bool) {
	out := map[string]float64{}
	end := m.s + m.k
	if method == Ritmo { // tal cual el método actual: todo lo gastado, sin separar fijos ni atípicos
		for _, c := range e.cats {
			s := e.byCat[c]
			out[c] = (sum(s.variable, m.s, end) + sum(s.fixed, m.s, end)) / float64(m.k) * float64(m.days)
		}
		return out, true
	}
	p := 0.0
	if method == Perfil {
		if p = e.profile(m); p == 0 {
			return nil, false
		}
	}
	for _, c := range e.cats {
		s := e.byCat[c]
		r, ok := e.remaining(method, s.variable, m, p)
		if !ok {
			return nil, false
		}
		out[c] = sum(s.variable, m.s, end) + r + sum(s.fixed, m.s, end) + upcoming[c]
	}
	return out, true
}

func total(m map[string]float64) float64 {
	var t float64
	for _, v := range m {
		t += v
	}
	return t
}

func ptr[T any](v T) *T { return &v }

func Project(in Input) *Result {
	e := newEngine(in)
	cur := monthStart(in.Today)
	m := month{start: cur, s: e.idx(cur), k: in.Today.Day(), days: daysIn(cur)}
	res := &Result{Month: cur.Format("2006-01"), Day: m.k, DaysInMonth: m.days, Methods: []Method{}, ByCategory: []Category{},
		Accuracy: []Accuracy{}, Outliers: e.outliers, OutlierAbove: e.thresh, Warnings: []string{}}
	if res.Outliers == nil {
		res.Outliers = []Outlier{}
	}

	// Backtest: cada mes cerrado evaluable, al mismo día (o el último si el mes es más corto).
	type eval struct {
		mo     month
		actual float64
		preds  map[string]float64 // por método (total); ausente = no disponible
	}
	var evals []eval
	for j := 1; j <= BacktestMonths; j++ {
		ms := cur.AddDate(0, -j, 0)
		if !e.evaluable(ms) {
			break
		}
		mo := month{start: ms, s: e.idx(ms), days: daysIn(ms)}
		mo.k = min(m.k, mo.days)
		end := mo.s + mo.days
		ev := eval{mo: mo, actual: sum(e.total.variable, mo.s, end) + sum(e.total.fixed, mo.s, end), preds: map[string]float64{}}
		// En un mes cerrado, los cobros fijos posteriores al día k ya estaban programados: se dan por conocidos.
		known := map[string]float64{}
		for _, c := range e.cats {
			known[c] = sum(e.byCat[c].recur, mo.s+mo.k, end)
		}
		for _, id := range Methods {
			if p, ok := e.predict(id, mo, known); ok {
				ev.preds[id] = total(p)
			}
		}
		evals = append(evals, ev)
	}

	// Mes en curso.
	upcoming := map[string]float64{}
	monthEnd := cur.AddDate(0, 1, 0)
	for _, u := range in.Upcoming {
		if !u.Day.Before(in.Today) && u.Day.Before(monthEnd) {
			upcoming[u.Category] += float64(u.Cents)
			res.UpcomingCents += u.Cents
		}
	}
	end := m.s + m.k
	spent := sum(e.total.variable, m.s, end) + sum(e.total.fixed, m.s, end)
	res.SpentCents = int64(math.Round(spent))
	current := map[string]map[string]float64{}
	for _, id := range Methods {
		met := Method{ID: id}
		if p, ok := e.predict(id, m, upcoming); ok && e.first < e.n {
			current[id] = p
			met.Available = true
			met.ProjectedCents = int64(math.Round(total(p)))
		}
		var errs []float64
		var pct []float64
		for _, ev := range evals {
			if p, ok := ev.preds[id]; ok {
				errs = append(errs, p-ev.actual)
				if ev.actual > 0 {
					pct = append(pct, math.Abs(p-ev.actual)/ev.actual*100)
				}
			}
		}
		met.Evaluated = len(errs)
		met.Candidate = len(evals) > 0 && len(errs) == len(evals)
		if len(errs) > 0 {
			var mae float64
			for _, x := range errs {
				mae += math.Abs(x)
			}
			met.MAECents = ptr(int64(math.Round(mae / float64(len(errs)))))
		}
		if len(pct) > 0 {
			var s float64
			for _, x := range pct {
				s += x
			}
			met.MAPE = ptr(math.Round(s/float64(len(pct))*10) / 10)
		}
		if met.Available {
			lo, hi, ok := e.interval(id, m, total(current[id]), errs)
			if ok {
				met.LowCents, met.HighCents = ptr(max(int64(math.Round(lo)), res.SpentCents)), ptr(int64(math.Round(hi)))
			}
		}
		res.Methods = append(res.Methods, met)
	}
	res.BaselineCents = res.Methods[0].ProjectedCents

	// Selección: menor error absoluto medio entre los métodos evaluables en todos los meses y disponibles hoy.
	res.Method = Ritmo
	if len(evals) >= 2 {
		best := math.Inf(1)
		for _, met := range res.Methods {
			if met.Candidate && met.Available && met.MAECents != nil && float64(*met.MAECents) < best {
				best, res.Method = float64(*met.MAECents), met.ID
			}
		}
	} else if e.first < e.n {
		res.Warnings = append(res.Warnings, "sin_meses_cerrados")
	}
	var chosen Method
	for _, met := range res.Methods {
		if met.ID == res.Method {
			chosen = met
		}
	}
	res.ProjectedCents = chosen.ProjectedCents
	res.LowCents, res.HighCents = chosen.LowCents, chosen.HighCents
	if res.LowCents == nil && chosen.Available && m.k < m.days { // sin rango propio: se simula el resto del mes con días reales
		rest := make([]int, 0, m.days-m.k)
		for j := m.s + m.k; j < m.s+m.days; j++ {
			rest = append(rest, j)
		}
		known := float64(res.SpentCents + res.UpcomingCents)
		rng := rand.New(rand.NewPCG(20261016, uint64(m.k)))
		if sims := e.simulate(res.Method, e.n, rest, float64(res.ProjectedCents)-known, horizonSims, rng); sims != nil {
			lo, _, hi := percentiles(sims, len(rest)-1)
			res.LowCents, res.HighCents = ptr(min(int64(math.Round(known+lo)), res.ProjectedCents)), ptr(max(int64(math.Round(known+hi)), res.ProjectedCents))
		}
	}
	if res.LowCents == nil { // tampoco hay historial para simular: se usa el rango de la regresión si existe
		if r := res.Methods[1]; r.Available && r.LowCents != nil {
			lo, hi := *r.LowCents, *r.HighCents
			shift := res.ProjectedCents - r.ProjectedCents
			res.LowCents, res.HighCents = ptr(max(lo+shift, res.SpentCents)), ptr(max(hi+shift, res.ProjectedCents))
		}
	}

	if e.first >= e.n {
		res.Warnings = append(res.Warnings, "sin_datos")
	} else {
		if m.k < MinRegressionDay {
			res.Warnings = append(res.Warnings, "pocos_dias")
		}
		if e.n-max(e.first, 0) < MinHistoryDays {
			res.Warnings = append(res.Warnings, "historial_corto")
		}
		if e.profile(m) == 0 {
			res.Warnings = append(res.Warnings, "pocos_meses_perfil")
		}
	}

	// Por categoría con el método elegido.
	if p := current[res.Method]; p != nil {
		for _, c := range e.cats {
			s := e.byCat[c]
			cs := sum(s.variable, m.s, end) + sum(s.fixed, m.s, end)
			row := Category{ID: c, SpentCents: int64(math.Round(cs)), UpcomingCents: int64(math.Round(upcoming[c])), ProjectedCents: int64(math.Round(p[c]))}
			if row.SpentCents != 0 || row.ProjectedCents != 0 || row.UpcomingCents != 0 {
				res.ByCategory = append(res.ByCategory, row)
			}
		}
		sort.SliceStable(res.ByCategory, func(i, j int) bool { return res.ByCategory[i].ProjectedCents > res.ByCategory[j].ProjectedCents })
	}

	// Precisión: proyección al mismo día contra el cierre real.
	for _, ev := range evals {
		p, ok := ev.preds[res.Method]
		if !ok {
			continue
		}
		a := Accuracy{Month: ev.mo.start.Format("2006-01"), Day: ev.mo.k, ActualCents: int64(math.Round(ev.actual)),
			PredictedCents: int64(math.Round(p)), BaselineCents: int64(math.Round(ev.preds[Ritmo]))}
		a.ErrorCents = a.PredictedCents - a.ActualCents
		if ev.actual > 0 {
			a.ErrorPct = ptr(math.Round(math.Abs(p-ev.actual)/ev.actual*1000) / 10)
			a.BaselinePct = ptr(math.Round(math.Abs(ev.preds[Ritmo]-ev.actual)/ev.actual*1000) / 10)
		}
		res.Accuracy = append(res.Accuracy, a)
	}

	// Acumulados para la gráfica.
	acc := 0.0
	res.Cumulative = make([]int64, m.k)
	for j := 0; j < m.k; j++ {
		acc += e.total.variable[m.s+j] + e.total.fixed[m.s+j]
		res.Cumulative[j] = int64(math.Round(acc))
	}
	prev := cur.AddDate(0, -1, 0)
	res.PreviousMonth = prev.Format("2006-01")
	ps, pd := e.idx(prev), daysIn(prev)
	res.PreviousCumulative = make([]int64, pd)
	acc = 0
	for j := 0; j < pd; j++ {
		acc += e.total.variable[ps+j] + e.total.fixed[ps+j]
		res.PreviousCumulative[j] = int64(math.Round(acc))
	}
	return res
}

// interval: la regresión usa su intervalo de predicción sobre el acumulado total; los demás, el error que tuvieron
// en los meses cerrados (con 3 o más), como intervalo de predicción empírico.
func (e *engine) interval(method string, m month, value float64, errs []float64) (lo, hi float64, ok bool) {
	if method == Regresion {
		y := make([]float64, m.k)
		acc := 0.0
		for j := 0; j < m.k; j++ {
			acc += e.total.variable[m.s+j]
			y[j] = acc
		}
		if _, half, ok := Regress(y, float64(m.days)); ok {
			return value - half, value + half, true
		}
		return 0, 0, false
	}
	if len(errs) < 3 {
		return 0, 0, false
	}
	var ss float64
	for _, x := range errs {
		ss += x * x
	}
	n := float64(len(errs))
	half := tValue(len(errs)-1) * math.Sqrt(ss/n) * math.Sqrt(1+1/n)
	return value - half, value + half, true
}

package forecast

import (
	"math"
	"math/rand/v2"
	"sort"
	"time"
)

// Proyección de un periodo futuro (la próxima semana o el próximo mes).
//
// El gasto variable de cada día se estima con varios métodos y se elige el que menos se equivocó en el pasado:
// se "retrocede" el reloj de 7 en 7 días (hasta 12 veces), se proyecta un periodo igual de largo y a la misma
// distancia con solo lo que se sabía entonces, y se compara con lo que realmente se gastó (validación con origen
// móvil). El rango del 95 % sale de simular el periodo muchas veces con días reales del historial (bootstrap), así
// recoge que el gasto es irregular: días sin nada y días con compras grandes. La cobertura mide en esos mismos
// periodos pasados cuántas veces el gasto real cayó dentro del rango.

const (
	HorizonEWMA     = "media_movil" // media ponderada exponencial (vida media de 21 días)
	HorizonWeekday  = "dia_semana"  // promedio por día de la semana, encogido hacia la media general
	HorizonWindow   = 90            // días de historial que usa cada método
	HorizonMinDays  = 7             // días completos mínimos para proyectar
	HorizonOrigins  = 12            // periodos pasados con los que se elige el método
	HorizonMinEval  = 3             // periodos pasados necesarios para elegir y para medir la cobertura
	horizonHalfLife = 21.0
	weekdayShrink   = 4.0  // días "prestados" de la media general en cada día de la semana
	horizonSims     = 2000 // simulaciones para el rango
	backtestSims    = 400
)

// HorizonMethods en orden de preferencia ante empates: primero el más simple.
var HorizonMethods = []string{Ritmo, HorizonEWMA, HorizonWeekday}

type HorizonMethod struct {
	ID             string `json:"id"`
	Available      bool   `json:"available"`
	ProjectedCents int64  `json:"projected_cents"`
	MAECents       *int64 `json:"mae_cents"`
	Evaluated      int    `json:"evaluated"`
}

type HorizonCategory struct {
	ID             string `json:"id"`
	ProjectedCents int64  `json:"projected_cents"`
	UpcomingCents  int64  `json:"upcoming_cents"`
}

type HorizonResult struct {
	From           string            `json:"from"`
	To             string            `json:"to"`
	Days           int               `json:"days"`
	HistoryDays    int               `json:"history_days"`
	Method         string            `json:"method"`
	ProjectedCents int64             `json:"projected_cents"`
	LowCents       *int64            `json:"low_cents"`
	HighCents      *int64            `json:"high_cents"`
	UpcomingCents  int64             `json:"upcoming_cents"` // gastos fijos programados en el periodo (ya incluidos)
	BaselineCents  int64             `json:"baseline_cents"` // método actual (ritmo), para comparar
	Methods        []HorizonMethod   `json:"methods"`
	ByCategory     []HorizonCategory `json:"by_category"`
	Backtests      int               `json:"backtests"`
	Coverage       *float64          `json:"coverage"` // fracción de periodos pasados dentro del rango del 95 %
	Outliers       int               `json:"outliers"` // compras atípicas recientes que no se extrapolan
	// Acumulado esperado y rango del 95 % al final de cada día del periodo.
	Cumulative []int64 `json:"cumulative"`
	CumLow     []int64 `json:"cumulative_low"`
	CumHigh    []int64 `json:"cumulative_high"`
}

// window: días de entrenamiento [from, o) para un origen o (índice del primer día que no se conoce).
func (e *engine) window(o, size int) (int, int) {
	from := max(o-size, e.first, 0)
	return from, o
}

// daily devuelve, para un método, el gasto variable esperado de cada día futuro (índices absolutos) con datos hasta o.
func (e *engine) daily(method string, v []float64, o int, days []int) ([]float64, bool) {
	from, to := e.window(o, HorizonWindow)
	n := to - from
	if n < HorizonMinDays {
		return nil, false
	}
	out := make([]float64, len(days))
	switch method {
	case Ritmo:
		r := sum(v, from, to) / float64(n)
		for i := range out {
			out[i] = r
		}
	case HorizonEWMA:
		if n < 14 {
			return nil, false
		}
		var num, den float64
		for i := from; i < to; i++ {
			w := math.Pow(0.5, float64(to-1-i)/horizonHalfLife)
			num += w * v[i]
			den += w
		}
		for i := range out {
			out[i] = num / den
		}
	case HorizonWeekday:
		wf, _ := e.window(o, 84)
		if to-wf < MinHistoryDays {
			return nil, false
		}
		var sums, counts [7]float64
		var all float64
		for i := wf; i < to; i++ {
			wd := e.start.AddDate(0, 0, i).Weekday()
			sums[wd] += v[i]
			counts[wd]++
			all += v[i]
		}
		mean := all / float64(to-wf)
		for i, d := range days {
			wd := e.start.AddDate(0, 0, d).Weekday()
			out[i] = (sums[wd] + weekdayShrink*mean) / (counts[wd] + weekdayShrink)
		}
	default:
		return nil, false
	}
	return out, true
}

// point: proyección variable por categoría (cada una en cero como mínimo: las devoluciones no generan gasto negativo).
func (e *engine) point(method string, o int, days []int) (map[string]float64, bool) {
	out := map[string]float64{}
	for _, c := range e.cats {
		d, ok := e.daily(method, e.byCat[c].variable, o, days)
		if !ok {
			return nil, false
		}
		var s float64
		for _, x := range d {
			s += x
		}
		out[c] = math.Max(0, s)
	}
	return out, true
}

// simulate arma sims periodos con días reales del historial [from, o) y los centra en el total esperado del método.
// Con el método por día de la semana, cada día se toma de días del historial con el mismo día de la semana. Cada
// simulación además remuestrea el historial para su propio nivel de gasto: así el rango incluye la incertidumbre de
// estimar el promedio con pocos días, no solo lo irregular que es cada día.
// Devuelve el acumulado de cada simulación al final de cada día (sims × len(days)).
func (e *engine) simulate(method string, o int, days []int, expected float64, sims int, rng *rand.Rand) [][]float64 {
	from, to := e.window(o, 84)
	if to-from < HorizonMinDays {
		return nil
	}
	var pool [7][]float64
	var all []float64
	var poolMean float64
	for i := from; i < to; i++ {
		wd := e.start.AddDate(0, 0, i).Weekday()
		pool[wd] = append(pool[wd], e.total.variable[i])
		all = append(all, e.total.variable[i])
		poolMean += e.total.variable[i]
	}
	poolMean /= float64(len(all))
	if poolMean <= 0 || expected <= 0 {
		return nil // sin gasto variable en el historial reciente: no hay forma del rango
	}
	scale := expected / (poolMean * float64(len(days)))
	out := make([][]float64, sims)
	for s := range out {
		var level float64
		for range all {
			level += all[rng.IntN(len(all))]
		}
		level /= float64(len(all)) * poolMean
		row := make([]float64, len(days))
		acc := 0.0
		for j, d := range days {
			src := all
			if p := pool[e.start.AddDate(0, 0, d).Weekday()]; method == HorizonWeekday && len(p) >= 4 {
				src = p
			}
			acc += src[rng.IntN(len(src))]
			row[j] = math.Max(0, acc*level*scale)
		}
		out[s] = row
	}
	return out
}

func percentiles(sims [][]float64, j int) (lo, mid, hi float64) {
	col := make([]float64, len(sims))
	for i, r := range sims {
		col[i] = r[j]
	}
	sort.Float64s(col)
	return quantile(col, 0.025), quantile(col, 0.5), quantile(col, 0.975)
}

// Horizon proyecta el gasto entre from y to (fechas locales, ambas incluidas, posteriores a hoy).
// in.Upcoming debe traer los cobros de gastos fijos programados en ese periodo.
func Horizon(in Input, from, to time.Time) *HorizonResult {
	e := newEngine(in)
	o := e.n - 1 // hoy no está completo: se entrena hasta ayer
	lead := dayNumber(from) - dayNumber(in.Today)
	h := dayNumber(to) - dayNumber(from) + 1
	res := &HorizonResult{From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Days: h, Method: Ritmo,
		Methods: []HorizonMethod{}, ByCategory: []HorizonCategory{}, Cumulative: []int64{}, CumLow: []int64{}, CumHigh: []int64{}}
	if e.first < o {
		res.HistoryDays = o - max(e.first, o-HorizonWindow)
	}
	for _, x := range e.outliers {
		if d, err := time.ParseInLocation("2006-01-02", x.Date, in.Today.Location()); err == nil && dayNumber(in.Today)-dayNumber(d) <= HorizonWindow {
			res.Outliers++
		}
	}
	if res.HistoryDays < HorizonMinDays || h <= 0 || lead < 1 {
		return res
	}
	rng := rand.New(rand.NewPCG(20261016, uint64(h)))
	at := func(o int) []int {
		d := make([]int, h)
		for j := range d {
			d[j] = o + lead + j
		}
		return d
	}

	// Validación con origen móvil: el periodo pasado tiene que haber terminado antes de hoy.
	type eval struct {
		o      int
		actual float64
		preds  map[string]float64
	}
	var evals []eval
	for j := 1; len(evals) < HorizonOrigins && j <= 60; j++ {
		ob := o - 7*j
		if ob+lead+h > o {
			continue
		}
		if ob-max(e.first, 0) < HorizonMinDays {
			break
		}
		days := at(ob)
		ev := eval{o: ob, actual: sum(e.total.variable, days[0], days[0]+h), preds: map[string]float64{}}
		for _, id := range HorizonMethods {
			if p, ok := e.point(id, ob, days); ok {
				ev.preds[id] = total(p)
			}
		}
		evals = append(evals, ev)
	}
	// Se comparan los métodos en los mismos periodos: aquellos en los que todos se podían calcular (los más simples
	// necesitan menos historial). Si no hay suficientes, solo el promedio diario tiene con qué evaluarse.
	var common []eval
	for _, ev := range evals {
		if len(ev.preds) == len(HorizonMethods) {
			common = append(common, ev)
		}
	}
	if len(common) >= HorizonMinEval {
		evals = common
	}
	res.Backtests = len(evals)

	upcoming := map[string]float64{}
	upDaily := make([]float64, h)
	for _, u := range in.Upcoming {
		j := dayNumber(u.Day) - dayNumber(from)
		if j >= 0 && j < h {
			upcoming[u.Category] += float64(u.Cents)
			upDaily[j] += float64(u.Cents)
			res.UpcomingCents += u.Cents
		}
	}
	days := at(o)
	points := map[string]map[string]float64{}
	best := math.Inf(1)
	for _, id := range HorizonMethods {
		m := HorizonMethod{ID: id}
		if p, ok := e.point(id, o, days); ok {
			points[id] = p
			m.Available = true
			m.ProjectedCents = int64(math.Round(total(p))) + res.UpcomingCents
		}
		var mae float64
		for _, ev := range evals {
			if p, ok := ev.preds[id]; ok {
				mae += math.Abs(p - ev.actual)
				m.Evaluated++
			}
		}
		if m.Evaluated > 0 {
			m.MAECents = ptr(int64(math.Round(mae / float64(m.Evaluated))))
		}
		// Compite solo si se pudo evaluar en todos los periodos pasados.
		if m.Available && len(evals) >= HorizonMinEval && m.Evaluated == len(evals) && mae/float64(m.Evaluated) < best {
			best, res.Method = mae/float64(m.Evaluated), id
		}
		res.Methods = append(res.Methods, m)
	}
	res.BaselineCents = res.Methods[0].ProjectedCents

	// Por categoría: la suma de las filas redondeadas es el total.
	p := points[res.Method]
	for _, c := range e.cats {
		row := HorizonCategory{ID: c, ProjectedCents: int64(math.Round(p[c] + upcoming[c])), UpcomingCents: int64(math.Round(upcoming[c]))}
		res.ProjectedCents += row.ProjectedCents
		res.ByCategory = append(res.ByCategory, row)
	}
	sort.SliceStable(res.ByCategory, func(i, j int) bool { return res.ByCategory[i].ProjectedCents > res.ByCategory[j].ProjectedCents })
	variable := float64(res.ProjectedCents - res.UpcomingCents)

	// Curva esperada: el gasto variable se reparte según el método día a día; los fijos caen en su fecha.
	d, _ := e.daily(res.Method, e.total.variable, o, days)
	var dsum float64
	for _, x := range d {
		dsum += math.Max(0, x)
	}
	acc, fixed := 0.0, 0.0
	for j := range days {
		share := 1 / float64(h)
		if dsum > 0 {
			share = math.Max(0, d[j]) / dsum
		}
		acc += variable * share
		fixed += upDaily[j]
		res.Cumulative = append(res.Cumulative, int64(math.Round(acc+fixed)))
	}

	sims := e.simulate(res.Method, o, days, variable, horizonSims, rng)
	if sims == nil {
		return res
	}
	fixed = 0
	for j := range days {
		fixed += upDaily[j]
		lo, _, hi := percentiles(sims, j)
		res.CumLow = append(res.CumLow, int64(math.Round(lo+fixed)))
		res.CumHigh = append(res.CumHigh, int64(math.Round(hi+fixed)))
	}
	res.LowCents, res.HighCents = ptr(min(res.CumLow[h-1], res.ProjectedCents)), ptr(max(res.CumHigh[h-1], res.ProjectedCents))

	// Cobertura: con el mismo método, ¿cuántas veces el gasto real del periodo pasado cayó en su rango?
	if len(evals) >= HorizonMinEval {
		in := 0
		for _, ev := range evals {
			bs := e.simulate(res.Method, ev.o, at(ev.o), ev.preds[res.Method], backtestSims, rng)
			if bs == nil { // sin gasto variable antes: el "rango" es cero
				if ev.actual <= 0.5 {
					in++
				}
				continue
			}
			lo, _, hi := percentiles(bs, h-1)
			if ev.actual >= lo-0.5 && ev.actual <= hi+0.5 {
				in++
			}
		}
		res.Coverage = ptr(math.Round(float64(in)/float64(len(evals))*100) / 100)
	}
	return res
}

package forecast

import (
	"math"
	"sort"
)

// Métodos añadidos para el gasto variable diario. Todos usan solo los días [from, to) anteriores al origen, así que
// se pueden evaluar en el pasado igual que los demás (validación temporal).
//
//   - Robusto: promedio de las semanas completas con el estimador de Huber, que recorta la influencia de las semanas
//     excepcionales sin descartarlas. Se calcula sobre el total y se reparte entre categorías según su peso en el
//     mismo periodo: en una categoría que gasta una vez al mes casi todas las semanas valen cero y un estimador robusto
//     por categoría la borraría.
//   - Holt amortiguado: suavizado exponencial del total semanal con tendencia que se apaga (φ < 1), para seguir un
//     cambio de nivel sin extrapolar la pendiente indefinidamente.
//   - Holt-Winters semanal: suavizado diario con nivel, tendencia amortiguada y un ajuste por día de la semana.
//
// Los parámetros de suavizado se eligen dentro de la ventana de entrenamiento (menor error a un paso), nunca mirando
// el periodo que se va a predecir.

const (
	Reciente    = "promedio_reciente" // fin de mes: lo gastado + promedio diario de los últimos 90 días × días restantes
	Robusto     = "robusto"
	Holt        = "holt"
	HoltWinters = "holt_winters"
	Combinado   = "combinado" // promedio de los pronósticos de los métodos de arriba que estén disponibles

	robustMinWeeks = 3
	holtMinWeeks   = 6
	hwMinDays      = 28
	hwWindow       = 84
	huberK         = 1.5
	// MinGain: un método más complejo solo reemplaza al más simple que va ganando si su error medio es al menos
	// un 10 % menor (y gana en más de la mitad de los periodos). En la evaluación (docs/PREDICCIONES.md), con
	// márgenes menores la selección perseguía ruido y quedaba peor que el promedio diario.
	MinGain = 0.10
)

// selectionGain es MinGain; la evaluación lo pone en cero para reproducir la selección anterior.
var selectionGain = MinGain

var requireMajority = true

// minDays: días completos de historial que necesita cada método del horizonte.
var minDays = map[string]int{Ritmo: HorizonMinDays, Robusto: 7 * robustMinWeeks, HorizonEWMA: 14, HorizonWeekday: MinHistoryDays, Holt: 7 * holtMinWeeks, HoltWinters: hwMinDays, Combinado: HorizonMinDays}

// combined: métodos que promedia Combinado. Combinar pronósticos distintos suele equivocarse menos que elegir uno.
var combined = []string{Ritmo, Robusto, HorizonEWMA, HorizonWeekday, Holt, HoltWinters}

// weekday del índice i de la ventana (0 = domingo, como time.Weekday).
func (e *engine) weekday(i int) int {
	return ((int(e.start.Weekday())+i)%7 + 7) % 7
}

// weeks devuelve los totales de las semanas completas que terminan en to (la más antigua primero), sin pasar de from.
func weeks(v []float64, from, to, limit int) []float64 {
	n := min((to-from)/7, limit)
	out := make([]float64, n)
	for k := 0; k < n; k++ {
		end := to - 7*(n-1-k)
		out[k] = sum(v, end-7, end)
	}
	return out
}

func median(x []float64) float64 {
	s := append([]float64(nil), x...)
	sort.Float64s(s)
	return quantile(s, 0.5)
}

// huber estima el centro de x con el estimador M de Huber (escala: MAD normalizada; si es cero, la desviación
// absoluta media respecto de la mediana).
func huber(x []float64) float64 {
	mu := median(x)
	dev := make([]float64, len(x))
	var mean float64
	for i, v := range x {
		dev[i] = math.Abs(v - mu)
		mean += dev[i]
	}
	scale := 1.4826 * median(dev)
	if scale == 0 {
		scale = mean / float64(len(x))
	}
	if scale == 0 {
		return mu
	}
	c := huberK * scale
	for it := 0; it < 50; it++ {
		var num, den float64
		for _, v := range x {
			w := 1.0
			if r := math.Abs(v - mu); r > c {
				w = c / r
			}
			num += w * v
			den += w
		}
		next := num / den
		if math.Abs(next-mu) < 1e-6 {
			return next
		}
		mu = next
	}
	return mu
}

// holtDamped ajusta Holt con tendencia amortiguada a y y devuelve los pronósticos a 1..steps pasos.
func holtDamped(y []float64, steps int) []float64 {
	best, out := math.Inf(1), make([]float64, steps)
	for _, a := range []float64{0.1, 0.2, 0.35, 0.5, 0.7} {
		for _, b := range []float64{0.02, 0.1, 0.2} {
			for _, phi := range []float64{0.8, 0.9, 0.98} {
				l, t := y[0], 0.0
				var sse float64
				for i := 1; i < len(y); i++ {
					f := l + phi*t
					if i >= 2 { // el primer paso solo inicializa la tendencia
						sse += (y[i] - f) * (y[i] - f)
					}
					nl := a*y[i] + (1-a)*f
					t = b*(nl-l) + (1-b)*phi*t
					l = nl
				}
				if sse < best-1e-9 {
					best = sse
					damp := 0.0
					for k := range out {
						damp += math.Pow(phi, float64(k+1))
						out[k] = l + damp*t
					}
				}
			}
		}
	}
	return out
}

// holtWinters ajusta Holt-Winters aditivo (estacionalidad de 7 días, tendencia amortiguada) a la serie diaria
// v[from:to) y devuelve el pronóstico de cada índice de days (posteriores o iguales a to).
func (e *engine) holtWinters(v []float64, from, to int, days []int) []float64 {
	var l float64
	for i := from; i < from+7; i++ {
		l += v[i]
	}
	l /= 7
	best := math.Inf(1)
	out := make([]float64, len(days))
	const phi = 0.9
	for _, a := range []float64{0.02, 0.05, 0.1, 0.2} {
		for _, b := range []float64{0, 0.01, 0.05} {
			for _, g := range []float64{0.05, 0.15, 0.3} {
				var s [7]float64
				for i := from; i < from+7; i++ {
					s[e.weekday(i)] = v[i] - l
				}
				lv, t := l, 0.0
				var sse float64
				for i := from + 7; i < to; i++ {
					wd := e.weekday(i)
					f := lv + phi*t + s[wd]
					if i >= from+14 {
						sse += (v[i] - f) * (v[i] - f)
					}
					nl := a*(v[i]-s[wd]) + (1-a)*(lv+phi*t)
					t = b*(nl-lv) + (1-b)*phi*t
					s[wd] = g*(v[i]-nl) + (1-g)*s[wd]
					lv = nl
				}
				if sse < best-1e-9 {
					best = sse
					for j, d := range days {
						h := d - to + 1
						damp := 0.0
						for k := 1; k <= h; k++ {
							damp += math.Pow(phi, float64(k))
						}
						out[j] = lv + damp*t + s[e.weekday(d)]
					}
				}
			}
		}
	}
	return out
}

// shares: peso de cada categoría en el gasto variable de [from, to) (sin negativos); nil si no hubo gasto.
func (e *engine) shares(from, to int) map[string]float64 {
	out := map[string]float64{}
	var all float64
	for _, c := range e.cats {
		v := math.Max(0, sum(e.byCat[c].variable, from, to))
		out[c] = v
		all += v
	}
	if all <= 0 {
		return nil
	}
	for c := range out {
		out[c] /= all
	}
	return out
}

// robustRate: gasto variable diario del total con Huber sobre las últimas 12 semanas completas.
func (e *engine) robustRate(o int) (float64, bool) {
	from, to := e.window(o, HorizonWindow)
	w := weeks(e.total.variable, from, to, 12)
	if len(w) < robustMinWeeks {
		return 0, false
	}
	return math.Max(0, huber(w)) / 7, true
}

// pick elige, recorriendo los métodos del más simple al más complejo, el de menor error medio. Uno más complejo
// solo reemplaza al que va ganando si su error medio es al menos MinGain menor y además se equivocó menos en más de
// la mitad de los periodos: así una racha de suerte en uno o dos periodos no basta. errs trae, por método, el error
// de cada periodo (alineados). Vacío si ninguno compite.
func pick(order []string, errs map[string][]float64, gain float64) string {
	best := ""
	meanAbs := func(x []float64) float64 {
		var s float64
		for _, v := range x {
			s += math.Abs(v)
		}
		return s / float64(len(x))
	}
	for _, id := range order {
		e, ok := errs[id]
		if !ok || len(e) == 0 {
			continue
		}
		if best == "" {
			best = id
			continue
		}
		b := errs[best]
		wins := 0
		for i := range e {
			if math.Abs(e[i]) < math.Abs(b[i]) {
				wins++
			}
		}
		if meanAbs(e) < meanAbs(b)*(1-gain) && (!requireMajority || 2*wins > len(e)) {
			best = id
		}
	}
	return best
}

// combine promedia, por categoría, los pronósticos disponibles de los métodos combinados.
func (e *engine) combine(o int, days []int) (map[string]float64, bool) {
	out := map[string]float64{}
	n := 0
	for _, id := range combined {
		p, ok := e.point(id, o, days)
		if !ok {
			continue
		}
		n++
		for c, v := range p {
			out[c] += v
		}
	}
	if n == 0 {
		return nil, false
	}
	for c := range out {
		out[c] /= float64(n)
	}
	return out, true
}

// combineDaily hace lo mismo con el pronóstico de cada día de una serie (para repartir el total en la curva).
func (e *engine) combineDaily(v []float64, o int, days []int) ([]float64, bool) {
	out := make([]float64, len(days))
	n := 0
	for _, id := range combined {
		d, ok := e.daily(id, v, o, days)
		if !ok {
			continue
		}
		n++
		for i, x := range d {
			out[i] += math.Max(0, x)
		}
	}
	if n == 0 {
		return nil, false
	}
	for i := range out {
		out[i] /= float64(n)
	}
	return out, true
}

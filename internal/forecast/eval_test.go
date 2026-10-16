package forecast

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// Evaluación reproducible de los métodos con historiales sintéticos (semillas fijas). Para cada escenario se
// "retrocede" el reloj semana a semana y se predice con solo lo que se sabía ese día; luego se compara con lo que
// el escenario realmente gastó. Se ejecuta con:
//
//	PULSE_FORECAST_EVAL=1 go test ./internal/forecast -run TestEvaluacion -v
//
// y, con PULSE_FORECAST_EVAL=ruta.md, además escribe la tabla en ese archivo (docs/PREDICCIONES.md la reproduce).

type escenario struct {
	id, nombre string
	gen        func(rng *rand.Rand, i int, d time.Time) []Tx // i: días desde el inicio
}

const evalDays = 420 // historial de cada escenario

// compra: monto log-normal en centavos con mediana m.
func compra(rng *rand.Rand, m float64) int64 {
	return int64(math.Round(m * math.Exp(0.6*rng.NormFloat64())))
}

// diarias: n compras Poisson(lambda) repartidas entre comida, transporte y ocio.
func diarias(rng *rand.Rand, lambda, m float64) []Tx {
	var out []Tx
	cats := []string{"comida", "comida", "transporte", "ocio"}
	for {
		lambda -= rng.ExpFloat64()
		if lambda < 0 {
			break
		}
		out = append(out, Tx{Category: cats[rng.IntN(len(cats))], Cents: compra(rng, m)})
	}
	return out
}

var escenarios = []escenario{
	{"estable", "Gasto estable con ruido", func(rng *rand.Rand, _ int, _ time.Time) []Tx { return diarias(rng, 1.3, 1200) }},
	{"semanal", "Fines de semana el triple", func(rng *rand.Rand, _ int, d time.Time) []Tx {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			return diarias(rng, 2.4, 2000)
		}
		return diarias(rng, 0.9, 900)
	}},
	{"tendencia", "Cambio de tendencia: sube 60 % en 4 meses", func(rng *rand.Rand, i int, _ time.Time) []Tx {
		f := 1.0
		if i > evalDays-120 {
			f += 0.6 * float64(i-(evalDays-120)) / 120
		}
		return diarias(rng, 1.2*f, 1200)
	}},
	{"nivel", "Cambio de nivel: baja a la mitad", func(rng *rand.Rand, i int, _ time.Time) []Tx {
		if i > evalDays-150 {
			return diarias(rng, 0.7, 1100)
		}
		return diarias(rng, 1.4, 1100)
	}},
	{"excepcionales", "Gastos excepcionales frecuentes", func(rng *rand.Rand, _ int, _ time.Time) []Tx {
		out := diarias(rng, 1.2, 1200)
		if rng.Float64() < 0.04 {
			out = append(out, Tx{Category: "ocio", Cents: compra(rng, 9000)})
		}
		return out
	}},
	{"escaso", "Pocos movimientos: días sin gasto", func(rng *rand.Rand, _ int, _ time.Time) []Tx {
		if rng.Float64() < 0.22 {
			return diarias(rng, 1.0, 3000)
		}
		return nil
	}},
	{"devoluciones", "Devoluciones frecuentes", func(rng *rand.Rand, _ int, _ time.Time) []Tx {
		out := diarias(rng, 1.3, 1200)
		if rng.Float64() < 0.12 {
			out = append(out, Tx{Category: "comida", Cents: -compra(rng, 1500)})
		}
		return out
	}},
	{"fijos", "Con gastos fijos mensuales", func(rng *rand.Rand, _ int, d time.Time) []Tx {
		out := diarias(rng, 1.1, 1100)
		if d.Day() == 5 {
			out = append(out, Tx{Category: "hogar", Cents: 45000, Recurring: true})
		}
		if d.Day() == 18 {
			out = append(out, Tx{Category: "ocio", Cents: 1599, Recurring: true})
		}
		return out
	}},
}

// serie genera el historial completo de un escenario, con fechas desde start.
func (s escenario) serie(start time.Time, seed uint64) []Tx {
	rng := rand.New(rand.NewPCG(seed, 7))
	var out []Tx
	for i := 0; i < evalDays; i++ {
		d := start.AddDate(0, 0, i)
		for _, t := range s.gen(rng, i, d) {
			t.Day = d
			t.Purchase = t.Cents > 0
			out = append(out, t)
		}
	}
	return out
}

// inputAt arma lo que Pulse sabría el día today: movimientos hasta hoy (hoy, parcial) y los fijos programados
// hasta until.
func inputAt(all []Tx, today, until time.Time) Input {
	in := Input{Today: today, Categories: []string{"comida", "transporte", "ocio", "hogar"}, FirstDay: all[0].Day}
	for _, t := range all {
		if t.Day.After(today) {
			if t.Recurring && !t.Day.After(until) {
				in.Upcoming = append(in.Upcoming, Upcoming{Category: t.Category, Day: t.Day, Cents: t.Cents})
			}
			continue
		}
		in.Txs = append(in.Txs, t)
	}
	return in
}

func spent(all []Tx, from, to time.Time) float64 {
	var s float64
	for _, t := range all {
		if !t.Day.Before(from) && !t.Day.After(to) {
			s += float64(t.Cents)
		}
	}
	return s
}

type marcador struct {
	abs, actual, hits, width float64
	n, ranged                int
	chosen                   map[string]int
}

func (m *marcador) add(pred, actual float64, lo, hi *int64, method string) {
	if m.chosen == nil {
		m.chosen = map[string]int{}
	}
	m.abs += math.Abs(pred - actual)
	m.actual += actual
	m.n++
	m.chosen[method]++
	if lo != nil && hi != nil {
		m.ranged++
		m.width += float64(*hi - *lo)
		if actual >= float64(*lo)-0.5 && actual <= float64(*hi)+0.5 {
			m.hits++
		}
	}
}

func (m *marcador) wape() float64 { return m.abs / m.actual * 100 }

func (m *marcador) coverage() string {
	if m.ranged == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f %%", m.hits/float64(m.ranged)*100)
}

func (m *marcador) top() string {
	type kv struct {
		k string
		v int
	}
	var l []kv
	for k, v := range m.chosen {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v || l[i].v == l[j].v && l[i].k < l[j].k })
	var parts []string
	for i, x := range l {
		if i == 2 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", x.k, x.v, m.n))
	}
	return strings.Join(parts, ", ")
}

var (
	metodosAnteriores    = []string{Ritmo, HorizonEWMA, HorizonWeekday}
	metodosMesAnteriores = []string{Ritmo, Regresion, MediaMovil, DiaSemana, Perfil}
)

type configuracion struct {
	nombre         string
	horizonte, mes []string
	gain           float64
	majority       bool
}

var configuraciones = []configuracion{
	{"Promedio diario (referencia)", []string{Ritmo}, []string{Ritmo}, 0, false},
	{"Selección anterior", metodosAnteriores, metodosMesAnteriores, 0, false},
	{"Selección nueva", HorizonMethods, Methods, MinGain, true},
}

// evaluar devuelve, por escenario y configuración, los marcadores de la próxima semana, el próximo mes (30 días)
// y el cierre del mes en curso.
func evaluar(t testing.TB, seeds, origins int) map[string][3][]*marcador {
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, gye)
	out := map[string][3][]*marcador{}
	defer func() { selectionGain, requireMajority = MinGain, true }()
	for si, s := range escenarios {
		var res [3][]*marcador
		for k := range res {
			res[k] = make([]*marcador, len(configuraciones))
			for c := range res[k] {
				res[k][c] = &marcador{}
			}
		}
		for seed := 0; seed < seeds; seed++ {
			all := s.serie(start, uint64(100+si+1000*seed))
			last := start.AddDate(0, 0, evalDays-31) // el último origen deja 30 días para comparar
			for o := 0; o < origins; o++ {
				today := last.AddDate(0, 0, -5*o) // cada 5 días: recorre todos los días de la semana y del mes
				for c, cfg := range configuraciones {
					selectionGain, requireMajority = cfg.gain, cfg.majority
					w := horizon(inputAt(all, today, today.AddDate(0, 0, 7)), today.AddDate(0, 0, 1), today.AddDate(0, 0, 7), cfg.horizonte)
					res[0][c].add(float64(w.ProjectedCents), spent(all, today.AddDate(0, 0, 1), today.AddDate(0, 0, 7)), w.LowCents, w.HighCents, w.Method)
					m := horizon(inputAt(all, today, today.AddDate(0, 0, 30)), today.AddDate(0, 0, 1), today.AddDate(0, 0, 30), cfg.horizonte)
					res[1][c].add(float64(m.ProjectedCents), spent(all, today.AddDate(0, 0, 1), today.AddDate(0, 0, 30)), m.LowCents, m.HighCents, m.Method)
					me := monthStart(today).AddDate(0, 1, -1)
					p := project(inputAt(all, today, me), cfg.mes)
					res[2][c].add(float64(p.ProjectedCents), spent(all, monthStart(today), me), p.LowCents, p.HighCents, p.Method)
				}
			}
		}
		out[s.id] = res
	}
	return out
}

func TestEvaluacion(t *testing.T) {
	dest := os.Getenv("PULSE_FORECAST_EVAL")
	if dest == "" {
		t.Skip("evaluación larga: PULSE_FORECAST_EVAL=1 para correrla")
	}
	const origins = 36
	r := evaluar(t, 3, origins)
	var b strings.Builder
	horizons := []string{"Próxima semana", "Próximos 30 días", "Cierre del mes en curso"}
	fmt.Fprintf(&b, "Escenarios sintéticos de %d días, 3 semillas por escenario y %d orígenes por semilla (uno cada 5 días). Error = suma de errores absolutos ÷ gasto real (WAPE). Cobertura = veces que el gasto real cayó dentro del rango del 95 %%.\n", evalDays, origins)
	var wins, total [3]int
	for k, h := range horizons {
		fmt.Fprintf(&b, "\n### %s\n\n| Escenario | Promedio diario | Selección anterior | Selección nueva | Cobertura anterior | Cobertura nueva | Métodos elegidos (nueva) |\n|---|---:|---:|---:|---:|---:|---|\n", h)
		for _, s := range escenarios {
			m := r[s.id][k]
			fmt.Fprintf(&b, "| %s | %.1f %% | %.1f %% | %.1f %% | %s | %s | %s |\n", s.nombre, m[0].wape(), m[1].wape(), m[2].wape(), m[1].coverage(), m[2].coverage(), m[2].top())
			total[k]++
			if m[2].wape() <= m[1].wape()+0.05 {
				wins[k]++
			}
		}
	}
	fmt.Fprintf(&b, "\nLa selección nueva iguala o mejora a la anterior en %d de %d escenarios (semana), %d de %d (30 días) y %d de %d (cierre de mes).\n", wins[0], total[0], wins[1], total[1], wins[2], total[2])
	t.Log("\n" + b.String())
	if dest != "1" {
		if err := os.WriteFile(dest, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Versión corta que corre siempre (8 orígenes, una semilla): en promedio la selección nueva no empeora a la anterior,
// no se aleja mucho del promedio diario (con tan pocos orígenes la diferencia es ruido) y sus rangos cubren la mayoría
// de los casos.
func TestEvaluacionCorta(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("simulación larga")
	}
	r := evaluar(t, 1, 8)
	for k := 0; k < 3; k++ {
		var prev, next, base, hits, ranged float64
		for _, s := range escenarios {
			m := r[s.id][k]
			base += m[0].wape()
			prev += m[1].wape()
			next += m[2].wape()
			hits += m[2].hits
			ranged += float64(m[2].ranged)
		}
		if ranged == 0 || hits/ranged < 0.8 {
			t.Errorf("horizonte %d: cobertura %.0f de %.0f", k, hits, ranged)
		}
		if next > prev*1.03 || next > base*1.15 {
			t.Errorf("horizonte %d: error medio nuevo %.1f, anterior %.1f, referencia %.1f", k, next/8, prev/8, base/8)
		}
	}
}

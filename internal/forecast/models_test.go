package forecast

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

func TestHuberReduceLoExcepcional(t *testing.T) {
	semanas := []float64{7000, 7200, 6900, 7100, 7000, 40000} // una semana con una compra excepcional
	var media float64
	for _, v := range semanas {
		media += v / float64(len(semanas))
	}
	h := huber(semanas)
	if h < 6900 || h > 7600 || media < 12000 {
		t.Fatalf("huber %.0f, media %.0f", h, media)
	}
	// Mitad de las semanas en cero (categoría esporádica): la escala no es cero y no se borra el gasto.
	if v := huber([]float64{0, 0, 0, 3000, 3000, 3000}); v <= 0 {
		t.Fatalf("esporádica: %.0f", v)
	}
	if v := huber([]float64{5, 5, 5}); v != 5 {
		t.Fatalf("constante: %v", v)
	}
}

func TestHoltSigueUnCambioDeNivel(t *testing.T) {
	y := []float64{100, 100, 100, 100, 100, 100, 200, 200, 200, 200, 200, 200}
	f := holtDamped(y, 2)
	if f[0] < 170 || f[0] > 240 || f[1] < 170 || f[1] > 260 {
		t.Fatalf("pronóstico %v", f)
	}
	// Sin tendencia, pronostica el nivel.
	if f := holtDamped([]float64{50, 50, 50, 50, 50, 50}, 3); math.Abs(f[2]-50) > 1e-6 {
		t.Fatalf("plano: %v", f)
	}
}

func TestHoltWintersSemanal(t *testing.T) {
	// Solo se gasta los sábados: el ajuste por día de la semana pone casi todo el gasto previsto en sábado.
	in := gen(hoy, hoy.AddDate(0, 0, -120), func(d time.Time) []Tx {
		if d.Weekday() == time.Saturday {
			return []Tx{{Cents: 7000}}
		}
		return nil
	})
	e := newEngine(in)
	o := e.n - 1
	days := []int{o + 3, o + 4, o + 5, o + 6, o + 7, o + 8, o + 9} // lunes 19 a domingo 25
	d, ok := e.daily(HoltWinters, e.total.variable, o, days)
	if !ok {
		t.Fatal("no disponible")
	}
	var sat, rest float64
	for i, x := range d {
		if e.weekday(days[i]) == int(time.Saturday) {
			sat += x
		} else {
			rest += math.Abs(x)
		}
	}
	if sat < 5000 || rest > 1500 {
		t.Fatalf("sábado %.0f, resto %.0f (%v)", sat, rest, d)
	}
}

func TestRobustoRepartePorCategoria(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	in := gen(hoy, hoy.AddDate(0, 0, -100), func(d time.Time) []Tx {
		txs := []Tx{{Cents: 800 + rng.Int64N(400)}}
		if d.Day() == 10 {
			txs = append(txs, Tx{Category: "hogar", Cents: 6000})
		}
		return txs
	})
	r := horizon(in, semanaIni, semanaFin, []string{Robusto})
	checkSums(t, r)
	if r.Method != Robusto && r.Backtests >= HorizonMinEval {
		t.Fatalf("método %s", r.Method)
	}
	var hogar int64
	for _, c := range r.ByCategory {
		if c.ID == "hogar" {
			hogar = c.ProjectedCents
		}
	}
	if hogar <= 0 {
		t.Fatalf("la categoría mensual no desaparece: %+v", r.ByCategory)
	}
}

func TestSeleccionNoPrefiereLoComplejo(t *testing.T) {
	// Errores casi iguales: gana el más simple.
	errs := map[string][]float64{Ritmo: {100, -100, 100, -100}, Holt: {95, -95, 95, -95}}
	if got := pick([]string{Ritmo, Holt}, errs, MinGain); got != Ritmo {
		t.Fatalf("eligió %s con solo 5 %% de mejora", got)
	}
	// Mejor en media gracias a un solo periodo: tampoco basta.
	errs = map[string][]float64{Ritmo: {100, 100, 100, 900}, Holt: {120, 120, 120, 100}}
	if got := pick([]string{Ritmo, Holt}, errs, MinGain); got != Ritmo {
		t.Fatalf("eligió %s por un periodo", got)
	}
	errs = map[string][]float64{Ritmo: {100, 100, 100, 100}, Holt: {50, 60, 40, 120}}
	if got := pick([]string{Ritmo, Holt}, errs, MinGain); got != Holt {
		t.Fatalf("no eligió al claramente mejor: %s", got)
	}
}

func TestCierreDeMesCoherente(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 9))
	in := gen(hoy, hoy.AddDate(0, -9, 0), func(d time.Time) []Tx {
		txs := []Tx{{Cents: 333 + rng.Int64N(2000)}, {Category: "hogar", Cents: 111 + rng.Int64N(700)}}
		if d.Day() == 1 {
			txs = append(txs, Tx{Category: "hogar", Cents: 50000, Recurring: true})
		}
		if rng.Float64() < 0.1 {
			txs = append(txs, Tx{Cents: -rng.Int64N(900)})
		}
		return txs
	})
	in.Upcoming = []Upcoming{{Category: "hogar", Day: day("2026-10-28"), Cents: 1999}}
	r := Project(in)
	var s int64
	for _, c := range r.ByCategory {
		s += c.ProjectedCents
	}
	if s != r.ProjectedCents {
		t.Fatalf("categorías %d, total %d", s, r.ProjectedCents)
	}
	// El fijo programado está una sola vez: lo gastado + lo pendiente nunca supera la proyección.
	if r.ProjectedCents < r.SpentCents+r.UpcomingCents || r.UpcomingCents != 1999 {
		t.Fatalf("%+v", r)
	}
	if r.Backtests < 2 || r.Confidence == "" || r.LowCents == nil || *r.LowCents > r.ProjectedCents || *r.HighCents < r.ProjectedCents {
		t.Fatalf("rango o confianza: %+v", r)
	}
}

func TestPocoHistorialEstimacionPrudente(t *testing.T) {
	// 20 días, ningún mes cerrado: se usa lo gastado + el promedio diario reciente, no el ritmo del mes.
	in := gen(hoy, hoy.AddDate(0, 0, -19), func(d time.Time) []Tx {
		if d.Day() == 1 { // un fijo cobrado el día 1 no se extrapola a todo el mes
			return []Tx{{Cents: 1000}, {Category: "hogar", Cents: 30000, Recurring: true}}
		}
		return []Tx{{Cents: 1000}}
	})
	r := Project(in)
	if r.Method != Reciente || r.Confidence != "baja" || !has(r.Warnings, "sin_meses_cerrados") {
		t.Fatalf("%+v", r)
	}
	// 16 días gastados (16 000 + 30 000) + 15 días × 1000 = 61 000; el ritmo daría (46 000 / 16) × 31.
	if r.ProjectedCents != 61000 || r.BaselineCents <= r.ProjectedCents {
		t.Fatalf("proyección %d, ritmo %d", r.ProjectedCents, r.BaselineCents)
	}
}

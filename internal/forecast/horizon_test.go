package forecast

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// Hoy es viernes 16 oct 2026: la próxima semana va del lunes 19 al domingo 25 y el próximo mes es noviembre.
var (
	hoy       = day("2026-10-16")
	semanaIni = day("2026-10-19")
	semanaFin = day("2026-10-25")
)

func checkSums(t *testing.T, r *HorizonResult) {
	t.Helper()
	var s int64
	for _, c := range r.ByCategory {
		s += c.ProjectedCents
	}
	if s != r.ProjectedCents {
		t.Fatalf("las categorías suman %d y el total es %d", s, r.ProjectedCents)
	}
	if len(r.Cumulative) != r.Days || (r.Days > 0 && len(r.Cumulative) > 0 && r.Cumulative[r.Days-1] != r.ProjectedCents) {
		t.Fatalf("acumulado %v no termina en %d", r.Cumulative, r.ProjectedCents)
	}
	if r.LowCents != nil && (*r.LowCents > r.ProjectedCents || *r.HighCents < r.ProjectedCents) {
		t.Fatalf("rango %d–%d no contiene %d", *r.LowCents, *r.HighCents, r.ProjectedCents)
	}
}

func TestHorizonteGastoConstante(t *testing.T) {
	in := gen(hoy, hoy.AddDate(0, 0, -150), func(time.Time) []Tx { return []Tx{{Cents: 1000}} })
	r := Horizon(in, semanaIni, semanaFin)
	checkSums(t, r)
	if r.Days != 7 || r.HistoryDays != 90 || r.Method != Ritmo || r.ProjectedCents != 7000 || r.BaselineCents != 7000 {
		t.Fatalf("semana: %+v", r)
	}
	// Todos los días iguales: el rango del 95 % no tiene ancho y siempre acierta.
	if *r.LowCents != 7000 || *r.HighCents != 7000 || r.Backtests != HorizonOrigins || r.Coverage == nil || *r.Coverage != 1 {
		t.Fatalf("rango y cobertura: %+v", r)
	}
	for _, m := range r.Methods {
		if !m.Available || m.ProjectedCents != 7000 || m.MAECents == nil || *m.MAECents != 0 {
			t.Fatalf("método %+v", m)
		}
	}
	m := Horizon(in, day("2026-11-01"), day("2026-11-30"))
	checkSums(t, m)
	if m.Days != 30 || m.ProjectedCents != 30000 {
		t.Fatalf("mes: %+v", m)
	}
}

func TestHorizontePocoHistorial(t *testing.T) {
	in := gen(hoy, hoy.AddDate(0, 0, -5), func(time.Time) []Tx { return []Tx{{Cents: 1000}} })
	r := Horizon(in, semanaIni, semanaFin)
	if r.HistoryDays != 5 || r.ProjectedCents != 0 || r.LowCents != nil || len(r.Methods) != 0 {
		t.Fatalf("con 5 días no se proyecta: %+v", r)
	}
	// Sin movimientos: nada, sin divisiones entre cero.
	vacio := Horizon(Input{Today: hoy, Categories: []string{"comida"}}, semanaIni, semanaFin)
	if vacio.HistoryDays != 0 || vacio.ProjectedCents != 0 {
		t.Fatalf("sin datos: %+v", vacio)
	}
	// 20 días: proyecta con el ritmo, pero aún no hay periodos pasados para elegir ni medir la cobertura.
	in = gen(hoy, hoy.AddDate(0, 0, -20), func(time.Time) []Tx { return []Tx{{Cents: 1000}} })
	r = Horizon(in, semanaIni, semanaFin)
	checkSums(t, r)
	if r.ProjectedCents != 7000 || r.Method != Ritmo || r.Coverage != nil || r.Backtests >= HorizonMinEval {
		t.Fatalf("historial corto: %+v", r)
	}
}

func TestHorizonteFijosAtipicosYDevoluciones(t *testing.T) {
	in := gen(hoy, hoy.AddDate(0, 0, -120), func(d time.Time) []Tx {
		txs := []Tx{{Cents: 1000}}
		if d.Day() == 20 {
			txs = append(txs, Tx{Category: "hogar", Cents: 5000, Recurring: true}) // el arriendo: no se reparte por día
		}
		if d.Equal(day("2026-10-01")) {
			txs = append(txs, Tx{Cents: 400000}) // un televisor: atípico, no se repite
		}
		if d.Equal(day("2026-10-05")) {
			txs = append(txs, Tx{Category: "hogar", Cents: -800}) // devolución
		}
		return txs
	})
	in.Upcoming = []Upcoming{{Category: "hogar", Day: day("2026-10-20"), Cents: 5000}, {Category: "hogar", Day: day("2026-11-20"), Cents: 5000}}
	r := Horizon(in, semanaIni, semanaFin)
	checkSums(t, r)
	if r.Outliers != 1 || r.UpcomingCents != 5000 || r.ProjectedCents != 12000 {
		t.Fatalf("fijos y atípicos: %+v", r)
	}
	for _, c := range r.ByCategory {
		// Hogar solo tuvo una devolución como gasto variable: se estima en cero más el arriendo programado.
		if c.ID == "hogar" && (c.ProjectedCents != 5000 || c.UpcomingCents != 5000) {
			t.Fatalf("hogar: %+v", c)
		}
	}
	// El arriendo aparece en su día en la curva acumulada (20 oct, segundo día del periodo).
	if r.Cumulative[0] != 1000 || r.Cumulative[1] != 7000 {
		t.Fatalf("curva: %v", r.Cumulative)
	}
}

func TestHorizontePorDiaDeLaSemana(t *testing.T) {
	// Solo se gasta los sábados.
	in := gen(hoy, hoy.AddDate(0, 0, -140), func(d time.Time) []Tx {
		if d.Weekday() == time.Saturday {
			return []Tx{{Cents: 7000}}
		}
		return nil
	})
	e := newEngine(in)
	o := e.n - 1
	days := []int{e.idx(day("2026-10-19")), e.idx(day("2026-10-24"))} // lunes y sábado
	d, ok := e.daily(HorizonWeekday, e.total.variable, o, days)
	if !ok || d[1] <= 5*d[0] {
		t.Fatalf("el sábado debe pesar mucho más que el lunes: %v", d)
	}
	r := Horizon(in, semanaIni, semanaFin)
	checkSums(t, r)
	// Cualquier semana completa tiene un sábado: todos aciertan y el rango del método por día de la semana es exacto.
	if r.ProjectedCents != 7000 || *r.LowCents > 7000 || *r.HighCents < 7000 {
		t.Fatalf("semana: %+v", r)
	}
	// Con el método por día de la semana la curva sube el sábado, no un poco cada día.
	if r.Method == HorizonWeekday && r.Cumulative[4] >= r.Cumulative[5] {
		t.Fatalf("curva: %v", r.Cumulative)
	}
}

func TestHorizonteRangoCalibrado(t *testing.T) {
	// Gasto irregular: muchos días sin nada y compras de tamaño log-normal. El 95 % debe cubrir la gran mayoría de
	// los periodos pasados, y el rango debe ser más ancho que cero.
	rng := rand.New(rand.NewPCG(7, 11))
	in := gen(hoy, hoy.AddDate(0, 0, -300), func(time.Time) []Tx {
		var txs []Tx
		for k := 0; k < 3; k++ {
			if rng.Float64() < 0.35 {
				txs = append(txs, Tx{Cents: int64(math.Exp(rng.NormFloat64()*0.8 + 7.5))})
			}
		}
		return txs
	})
	for _, p := range [][2]time.Time{{semanaIni, semanaFin}, {day("2026-11-01"), day("2026-11-30")}} {
		r := Horizon(in, p[0], p[1])
		checkSums(t, r)
		if r.Backtests != HorizonOrigins || r.Coverage == nil || *r.Coverage < 0.75 {
			t.Fatalf("%s: cobertura %+v", r.From, r)
		}
		if *r.HighCents-*r.LowCents < r.ProjectedCents/10 || len(r.CumLow) != r.Days {
			t.Fatalf("%s: rango demasiado estrecho %+v", r.From, r)
		}
		// El método elegido no se equivocó más que el método actual en los periodos pasados.
		var chosen, base *int64
		for _, m := range r.Methods {
			if m.Evaluated != r.Backtests {
				t.Fatalf("%s: todos los métodos se comparan en los mismos periodos: %+v", r.From, m)
			}
			if m.ID == r.Method {
				chosen = m.MAECents
			}
			if m.ID == Ritmo {
				base = m.MAECents
			}
		}
		if *chosen > *base {
			t.Fatalf("%s: elegido %d > ritmo %d", r.From, *chosen, *base)
		}
		// Determinista: misma entrada, mismo resultado.
		if again := Horizon(in, p[0], p[1]); *again.LowCents != *r.LowCents || *again.HighCents != *r.HighCents {
			t.Fatal("el rango cambia entre llamadas")
		}
	}
}

func TestHorizonteCambioDeHorario(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	today := time.Date(2026, 10, 30, 0, 0, 0, 0, ny) // el horario cambia el domingo 1 de noviembre
	in := Input{Today: today, Categories: []string{"comida"}, FirstDay: today.AddDate(0, 0, -60)}
	for d := today.AddDate(0, 0, -60); !d.After(today); d = d.AddDate(0, 0, 1) {
		in.Txs = append(in.Txs, Tx{Category: "comida", Day: d, Cents: 1000, Purchase: true})
	}
	r := Horizon(in, time.Date(2026, 11, 1, 0, 0, 0, 0, ny), time.Date(2026, 11, 30, 0, 0, 0, 0, ny))
	if r.Days != 30 || r.ProjectedCents != 30000 {
		t.Fatalf("cambio de horario: %+v", r)
	}
}

func TestHorizonteComparaEnLosMismosPeriodos(t *testing.T) {
	// 60 días: el día de la semana necesita 28 de historial, así que en los periodos más antiguos no se puede calcular.
	// La comparación usa solo los periodos en que todos los métodos existían.
	rng := rand.New(rand.NewPCG(3, 5))
	in := gen(hoy, hoy.AddDate(0, 0, -60), func(time.Time) []Tx {
		if rng.Float64() < 0.5 {
			return []Tx{{Cents: 500 + rng.Int64N(3000)}}
		}
		return nil
	})
	r := Horizon(in, semanaIni, semanaFin)
	if r.Backtests < HorizonMinEval {
		t.Fatalf("periodos: %+v", r)
	}
	for _, m := range r.Methods {
		if m.Evaluated != r.Backtests {
			t.Fatalf("método %s evaluado en %d de %d", m.ID, m.Evaluated, r.Backtests)
		}
	}
}

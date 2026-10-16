package forecast

import (
	"math"
	"testing"
	"time"
)

var gye, _ = time.LoadLocation("America/Guayaquil")

func day(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, gye)
	if err != nil {
		panic(err)
	}
	return t
}

// gen crea movimientos diarios desde el inicio de la ventana hasta hoy (inclusive).
func gen(today time.Time, from time.Time, f func(d time.Time) []Tx) Input {
	in := Input{Today: today, Categories: []string{"comida", "hogar"}}
	if from.IsZero() {
		from = WindowStart(today)
	}
	for d := from; !d.After(today); d = d.AddDate(0, 0, 1) {
		for _, t := range f(d) {
			t.Day = d
			if t.Category == "" {
				t.Category = "comida"
			}
			t.Purchase = t.Purchase || t.Cents > 0
			in.Txs = append(in.Txs, t)
			if in.FirstDay.IsZero() {
				in.FirstDay = d
			}
		}
	}
	return in
}

func method(r *Result, id string) Method {
	for _, m := range r.Methods {
		if m.ID == id {
			return m
		}
	}
	panic(id)
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestRegresionCalculadaAMano(t *testing.T) {
	// x̄ = 3, ȳ = 36, Sxx = 10, Sxy = 120 → b = 12, a = 0; SSE = 80, s = √(80/3);
	// mitad del intervalo en x = 30: t(3) · s · √(1 + 1/5 + 27²/10) = 3,182 · 5,16398 · 8,60814 = 141,446.
	pred, half, ok := Regress([]float64{10, 30, 30, 50, 60}, 30)
	if !ok || math.Abs(pred-360) > 1e-9 || math.Abs(half-141.446) > 0.01 {
		t.Fatalf("pred=%v half=%v ok=%v", pred, half, ok)
	}
	// Recta perfecta: sin incertidumbre.
	if pred, half, _ := Regress([]float64{5, 10, 15, 20}, 10); math.Abs(pred-50) > 1e-9 || half != 0 {
		t.Fatalf("recta perfecta: %v ± %v", pred, half)
	}
	for _, y := range [][]float64{nil, {1}, {1, 2}} {
		if _, _, ok := Regress(y, 30); ok {
			t.Fatalf("%v puntos no alcanzan", len(y))
		}
	}
	if tValue(0) != math.Inf(1) || tValue(1) != 12.706 || tValue(30) != 2.042 || tValue(100) >= 2.042 || tValue(100) <= 1.96 {
		t.Fatal("tabla t")
	}
}

func TestSinDatosYPocosDias(t *testing.T) {
	r := Project(Input{Today: day("2026-10-02")})
	if r.ProjectedCents != 0 || !has(r.Warnings, "sin_datos") || r.Method != Ritmo || len(r.Cumulative) != 2 || len(r.PreviousCumulative) != 30 {
		t.Fatalf("sin datos: %+v", r)
	}
	for _, m := range r.Methods {
		if m.Available {
			t.Fatalf("%s no debería estar disponible sin datos", m.ID)
		}
	}
	// Usuario nuevo: empezó ayer y gastó 30 y 10.
	today := day("2026-10-02")
	in := gen(today, day("2026-10-01"), func(d time.Time) []Tx {
		if d.Day() == 1 {
			return []Tx{{Cents: 3000}}
		}
		return []Tx{{Cents: 1000}}
	})
	r = Project(in)
	if r.Method != Ritmo || r.ProjectedCents != 4000*31/2 || r.SpentCents != 4000 {
		t.Fatalf("pocos días: %s %d", r.Method, r.ProjectedCents)
	}
	for _, w := range []string{"pocos_dias", "historial_corto", "sin_meses_cerrados", "pocos_meses_perfil"} {
		if !has(r.Warnings, w) {
			t.Errorf("falta el aviso %s: %v", w, r.Warnings)
		}
	}
	if method(r, Regresion).Available || method(r, MediaMovil).Available || method(r, DiaSemana).Available || method(r, Perfil).Available {
		t.Fatal("con dos días solo el ritmo es posible")
	}
	if r.LowCents != nil {
		t.Fatal("sin historial no hay rango")
	}
}

func TestGastoParejoTodosAciertan(t *testing.T) {
	today := day("2026-10-15")
	r := Project(gen(today, time.Time{}, func(time.Time) []Tx { return []Tx{{Cents: 1000}} }))
	if r.DaysInMonth != 31 || r.Day != 15 || r.SpentCents != 15000 {
		t.Fatalf("%+v", r)
	}
	for _, m := range r.Methods {
		if !m.Available || !m.Candidate || m.Evaluated != BacktestMonths || m.ProjectedCents != 31000 {
			t.Errorf("%s: %+v", m.ID, m)
		}
		if *m.MAECents != 0 {
			t.Errorf("%s: error %d con gasto parejo", m.ID, *m.MAECents)
		}
	}
	if r.Method != Ritmo || len(r.Accuracy) != BacktestMonths || r.Accuracy[0].Month != "2026-09" || r.Accuracy[0].ActualCents != 30000 {
		t.Fatalf("selección/precisión: %s %+v", r.Method, r.Accuracy)
	}
	// Regresión con recta perfecta: rango de ancho cero.
	if r.LowCents == nil || *r.LowCents != 31000 || *r.HighCents != 31000 {
		t.Fatalf("rango: %v %v", r.LowCents, r.HighCents)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("avisos: %v", r.Warnings)
	}
}

func TestDiaDeLaSemanaGanaAlRitmo(t *testing.T) {
	// Solo gasta los sábados: el ritmo se equivoca según en qué día cae el corte.
	today := day("2026-10-13") // martes
	r := Project(gen(today, time.Time{}, func(d time.Time) []Tx {
		if d.Weekday() == time.Saturday {
			return []Tx{{Cents: 7000}}
		}
		return nil
	}))
	ritmo, chosen := method(r, Ritmo), method(r, r.Method)
	if r.Method == Ritmo || *chosen.MAECents > *ritmo.MAECents {
		t.Fatalf("eligió %s (%d) vs ritmo %d", r.Method, *chosen.MAECents, *ritmo.MAECents)
	}
	// Octubre 2026 tiene 5 sábados: 3, 10, 17, 24 y 31.
	if dw := method(r, DiaSemana); dw.ProjectedCents != 35000 {
		t.Fatalf("día de la semana: %d", dw.ProjectedCents)
	}
	for _, a := range r.Accuracy {
		if math.Abs(float64(a.ErrorCents)) > math.Abs(float64(a.BaselineCents-a.ActualCents))+1 && r.Method == DiaSemana {
			t.Errorf("%s: el elegido no debería perder contra el ritmo en este patrón", a.Month)
		}
	}
}

func TestGastosFijosConocidosYAtipicos(t *testing.T) {
	today := day("2026-10-10")
	rent := func(d time.Time) bool { return d.Day() == 28 }
	in := gen(today, time.Time{}, func(d time.Time) []Tx {
		out := []Tx{{Cents: 1000 + int64(d.Day()%3)*100}}
		if rent(d) {
			out = append(out, Tx{Category: "hogar", Cents: 50000, Recurring: true})
		}
		return out
	})
	// Un gasto enorme hoy: se lista como atípico y no se extrapola.
	in.Txs = append(in.Txs, Tx{Category: "hogar", Day: today, Cents: 400000, Purchase: true})
	in.Upcoming = []Upcoming{{Category: "hogar", Day: day("2026-10-28"), Cents: 50000}, {Category: "hogar", Day: day("2026-11-28"), Cents: 50000}}
	r := Project(in)
	if r.UpcomingCents != 50000 {
		t.Fatalf("solo cuenta el cobro de este mes: %d", r.UpcomingCents)
	}
	if len(r.Outliers) != 1 || r.Outliers[0].Cents != 400000 || r.OutlierAbove == nil {
		t.Fatalf("atípicos: %+v", r.Outliers)
	}
	if r.Method == Ritmo {
		t.Fatal("con el arriendo a fin de mes el ritmo no debería ganar")
	}
	// variable ≈ 31 × 1100 + atípico + arriendo.
	want := 31*1100 + 400000 + 50000
	if math.Abs(float64(r.ProjectedCents-int64(want))) > 1500 {
		t.Fatalf("proyección %d, esperaba ≈ %d", r.ProjectedCents, want)
	}
	// El ritmo extrapola el atípico (×3,1): por eso se compara y se descarta.
	if method(r, Ritmo).ProjectedCents < 1_000_000 {
		t.Fatalf("ritmo: %d", method(r, Ritmo).ProjectedCents)
	}
	var hogar *Category
	for i := range r.ByCategory {
		if r.ByCategory[i].ID == "hogar" {
			hogar = &r.ByCategory[i]
		}
	}
	if hogar == nil || hogar.UpcomingCents != 50000 || hogar.ProjectedCents != 450000 || hogar.SpentCents != 400000 {
		t.Fatalf("hogar: %+v", hogar)
	}
	var sum int64
	for _, c := range r.ByCategory {
		sum += c.ProjectedCents
	}
	if math.Abs(float64(sum-r.ProjectedCents)) > float64(len(r.ByCategory)) {
		t.Fatalf("las categorías (%d) no suman el total (%d)", sum, r.ProjectedCents)
	}
}

func TestDevolucionesNoProyectanNegativo(t *testing.T) {
	today := day("2026-10-20")
	r := Project(gen(today, time.Time{}, func(d time.Time) []Tx {
		if d.Month() == time.October && d.Day() == 5 {
			return []Tx{{Cents: 1000}, {Category: "hogar", Cents: -5000}}
		}
		return []Tx{{Cents: 1000}}
	}))
	for _, m := range r.Methods {
		if m.Available && m.ProjectedCents < r.SpentCents {
			t.Errorf("%s proyecta %d por debajo de lo gastado %d", m.ID, m.ProjectedCents, r.SpentCents)
		}
	}
	if r.SpentCents != 20000-5000 {
		t.Fatalf("gastado neto: %d", r.SpentCents)
	}
}

func TestMesesBisiestosYCortos(t *testing.T) {
	flat := func(time.Time) []Tx { return []Tx{{Cents: 100}} }
	r := Project(gen(day("2028-02-15"), time.Time{}, flat))
	if r.DaysInMonth != 29 || r.ProjectedCents != 2900 || len(r.PreviousCumulative) != 31 {
		t.Fatalf("febrero bisiesto: %+v", r)
	}
	// El 31 de marzo, febrero se evalúa en su último día (29): la proyección es exacta.
	r = Project(gen(day("2028-03-31"), time.Time{}, flat))
	var feb *Accuracy
	for i := range r.Accuracy {
		if r.Accuracy[i].Month == "2028-02" {
			feb = &r.Accuracy[i]
		}
	}
	if feb == nil || feb.Day != 29 || feb.ActualCents != 2900 || feb.ErrorCents != 0 {
		t.Fatalf("febrero en el backtest: %+v", feb)
	}
	r = Project(gen(day("2027-02-28"), time.Time{}, flat))
	if r.DaysInMonth != 28 || r.ProjectedCents != 2800 {
		t.Fatalf("febrero normal: %+v", r)
	}
}

func TestCambioDeHorario(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	// 1 de noviembre de 2026 termina el horario de verano: el día tiene 25 horas.
	today := time.Date(2026, 11, 3, 0, 0, 0, 0, ny)
	in := Input{Today: today, FirstDay: time.Date(2026, 1, 1, 0, 0, 0, 0, ny), Categories: []string{"c"}}
	for d := time.Date(2026, 10, 1, 0, 0, 0, 0, ny); !d.After(today); d = d.AddDate(0, 0, 1) {
		in.Txs = append(in.Txs, Tx{Category: "c", Day: d, Cents: 100, Purchase: true})
	}
	r := Project(in)
	if r.Day != 3 || r.DaysInMonth != 30 || r.SpentCents != 300 || len(r.PreviousCumulative) != 31 || r.PreviousCumulative[30] != 3100 {
		t.Fatalf("%+v", r)
	}
}

func TestGastoSemanalGrandeNoEsAtipico(t *testing.T) {
	today := day("2026-10-13")
	r := Project(gen(today, time.Time{}, func(d time.Time) []Tx {
		if d.Weekday() == time.Saturday {
			return []Tx{{Cents: 7000}} // supermercado semanal: 15 veces la compra típica, pero habitual
		}
		if d.Day()%5 == 0 {
			return []Tx{{Cents: 2000}}
		}
		return []Tx{{Cents: 300 + int64(d.YearDay()%7)*50}}
	}))
	if len(r.Outliers) != 0 {
		t.Fatalf("un gasto habitual no es atípico: %+v (umbral %d)", r.Outliers, *r.OutlierAbove)
	}
	if r.Method != DiaSemana || *method(r, DiaSemana).MAECents >= *method(r, Ritmo).MAECents {
		t.Fatalf("eligió %s", r.Method)
	}
	if r.LowCents == nil || !(*r.LowCents < r.ProjectedCents && r.ProjectedCents < *r.HighCents) {
		t.Fatal("el método elegido debe mostrar un rango que contenga el valor")
	}
}

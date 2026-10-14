package budget

import (
	"reflect"
	"testing"
	"time"
)

var gye, _ = time.LoadLocation("America/Guayaquil")

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, gye)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMonthRangeEnZonaLocal(t *testing.T) {
	s, e, err := MonthRange("2026-10", gye)
	if err != nil {
		t.Fatal(err)
	}
	if s.Format(time.RFC3339) != "2026-10-01T05:00:00Z" || e.Format(time.RFC3339) != "2026-11-01T05:00:00Z" {
		t.Fatalf("rango incorrecto %v – %v", s, e)
	}
	// 31 oct 23:30 en Guayaquil = 1 nov 04:30 UTC, pero pertenece a octubre
	g := at("2026-10-31 23:30")
	if MonthOf(g, gye) != "2026-10" || !(g.UTC().Before(e) && !g.UTC().Before(s)) {
		t.Fatal("un gasto a las 23:30 del 31 debe caer en octubre")
	}
	for _, bad := range []string{"", "2026-13", "2026-1", "26-10", "1999-12", "abcd-ef"} {
		if _, _, err := MonthRange(bad, gye); err == nil {
			t.Errorf("%q debería ser inválido", bad)
		}
	}
}

func TestDiasDelMes(t *testing.T) {
	cases := map[string]int{"2026-02": 28, "2028-02": 29, "2026-04": 30, "2026-10": 31, "2026-12": 31, "2100-02": 28, "2000-02": 29}
	for m, want := range cases {
		if got := DaysIn(m, gye); got != want {
			t.Errorf("%s: %d días, esperaba %d", m, got, want)
		}
	}
}

func TestProyeccion(t *testing.T) {
	cases := []struct {
		name            string
		budget, spent   int64
		month           string
		now             time.Time
		projected, over int64
		elapsed         int
		status          string
	}{
		{"día 1 sin gastos", 10000, 0, "2026-10", at("2026-10-01 00:05"), 0, 0, 1, "ok"},
		{"día 1 con un gasto", 10000, 1000, "2026-10", at("2026-10-01 10:00"), 31000, 21000, 1, "warning"},
		{"mitad de mes", 31000, 15000, "2026-10", at("2026-10-15 12:00"), 31000, 0, 15, "ok"},
		{"último día", 10000, 9000, "2026-10", at("2026-10-31 23:59"), 9000, 0, 31, "warning"},
		{"febrero bisiesto", 29000, 1000, "2028-02", at("2028-02-01 08:00"), 29000, 0, 1, "ok"},
		{"febrero normal día 14", 28000, 14000, "2026-02", at("2026-02-14 08:00"), 28000, 0, 14, "ok"},
		{"mes pasado", 10000, 12000, "2026-09", at("2026-10-14 08:00"), 12000, 2000, 30, "over"},
		{"mes futuro", 10000, 0, "2026-11", at("2026-10-14 08:00"), 0, 0, 0, "ok"},
		{"excedido", 10000, 10001, "2026-10", at("2026-10-20 08:00"), 15502, 5502, 20, "over"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Compute(c.budget, c.spent, c.month, c.now, gye)
			if p.ProjectedCents != c.projected || p.ProjectedOverCents != c.over || p.DaysElapsed != c.elapsed || p.Status != c.status {
				t.Fatalf("got proj=%d over=%d días=%d estado=%s; want %d %d %d %s",
					p.ProjectedCents, p.ProjectedOverCents, p.DaysElapsed, p.Status, c.projected, c.over, c.elapsed, c.status)
			}
			if p.RemainingCents != c.budget-c.spent {
				t.Fatal("restante incorrecto")
			}
		})
	}
}

func TestUmbrales(t *testing.T) {
	th, err := ParseThresholds(" 100, 50 ,80,50")
	if err != nil || !reflect.DeepEqual(th, []int{50, 80, 100}) {
		t.Fatalf("got %v %v", th, err)
	}
	for _, bad := range []string{"", "0", "201", "a", "1,2,3,4,5,6"} {
		if _, err := ParseThresholds(bad); err == nil {
			t.Errorf("%q debería fallar", bad)
		}
	}
	if FormatThresholds(th) != "50,80,100" {
		t.Fatal("formato")
	}
	if got := Crossed(th, 10000, 7999); !reflect.DeepEqual(got, []int{50}) {
		t.Fatalf("7999/10000 → %v", got)
	}
	if got := Crossed(th, 10000, 8000); !reflect.DeepEqual(got, []int{50, 80}) {
		t.Fatalf("8000/10000 → %v (80%% exacto debe contar)", got)
	}
	if got := Crossed(th, 0, 100); got != nil {
		t.Fatal("presupuesto 0 no cruza nada")
	}
}

func FuzzCompute(f *testing.F) {
	f.Add(int64(10000), int64(500), int64(1_790_000_000))
	f.Fuzz(func(t *testing.T, b, s, ts int64) {
		if b <= 0 || b > 1e8 || s < -1e10 || s > 1e10 {
			return
		}
		now := time.Unix(ts%4_000_000_000, 0)
		p := Compute(b, s, "2026-10", now, gye)
		if s >= 0 && p.ProjectedCents < s {
			t.Fatalf("la proyección (%d) no puede ser menor a lo gastado (%d)", p.ProjectedCents, s)
		}
	})
}

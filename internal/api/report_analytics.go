package api

import (
	"math"
	"sort"
	"time"

	"pulse/internal/forecast"
	"pulse/internal/store"
)

type reportIDTotal struct {
	ID         string `json:"id"`
	SpentCents int64  `json:"spent_cents"`
}

// reportAccountCategory: gasto del periodo de una cuenta en una categoría principal.
type reportAccountCategory struct {
	AccountID  string `json:"account_id"`
	CategoryID string `json:"category_id"`
	SpentCents int64  `json:"spent_cents"`
	Count      int    `json:"purchase_count"`
}

type reportMonth struct {
	Month      string          `json:"month"`
	SpentCents int64           `json:"spent_cents"`
	ByCategory []reportIDTotal `json:"by_category"`
	ByAccount  []reportIDTotal `json:"by_account"`
}

// reportWeekday: gasto del periodo por día de la semana (0 = lunes). Days cuenta cuántos de esos días hubo,
// para promediar sin dividir entre cero.
type reportWeekday struct {
	Weekday    int   `json:"weekday"`
	SpentCents int64 `json:"spent_cents"`
	Days       int   `json:"days"`
	AvgCents   int64 `json:"avg_cents"`
}

// reportDay alimenta el mapa de calor de los últimos 91 días (13 semanas), hoy incluido.
type reportDay struct {
	Date       string `json:"date"`
	SpentCents int64  `json:"spent_cents"`
}

type reportForecastCategory struct {
	ID         string `json:"id"`
	WeekCents  int64  `json:"week_cents"`
	MonthCents int64  `json:"month_cents"`
}

type reportForecast struct {
	Ready       bool                     `json:"ready"`
	HistoryDays int                      `json:"history_days"`
	HistoryFrom string                   `json:"history_from"`
	HistoryTo   string                   `json:"history_to"`
	WeekFrom    string                   `json:"week_from"`
	WeekTo      string                   `json:"week_to"`
	MonthFrom   string                   `json:"month_from"`
	MonthTo     string                   `json:"month_to"`
	MonthDays   int                      `json:"month_days"`
	WeekCents   int64                    `json:"week_cents"`
	MonthCents  int64                    `json:"month_cents"`
	ByCategory  []reportForecastCategory `json:"by_category"`
	// Modelo con selección por validación y rango del 95 % (nil sin historial suficiente). Las cifras de arriba son
	// el método anterior (promedio diario), que se conserva como referencia.
	Week  *forecast.HorizonResult `json:"week_model"`
	Month *forecast.HorizonResult `json:"month_model"`
}

type reportSummaryData struct {
	From          string          `json:"from"`
	To            string          `json:"to"`
	TotalCents    int64           `json:"total_cents"`
	PurchaseCents int64           `json:"purchase_cents"`
	RefundCents   int64           `json:"refund_cents"`
	PurchaseCount int             `json:"purchase_count"`
	ElapsedDays   int             `json:"elapsed_days"`
	Months        []reportMonth   `json:"months"`
	ByCategory    []reportIDTotal `json:"by_category"`
	ByAccount     []reportIDTotal `json:"by_account"`
	Forecast      reportForecast  `json:"forecast"`
	Weekdays      []reportWeekday `json:"weekdays"`
	Days          []reportDay     `json:"days"`
	// Por cuenta y categoría principal, y número de compras de cada cuenta en el periodo.
	AccountCategories []reportAccountCategory `json:"account_categories"`
	AccountPurchases  map[string]int          `json:"account_purchases"`
	// Filtros aplicados, periodo transcurrido, periodo anterior equivalente, serie de evolución, cambio por
	// categoría y comercios con más gasto.
	Filters         reportFilter     `json:"filters"`
	Period          reportPeriod     `json:"period"`
	Previous        reportPrevious   `json:"previous"`
	Series          []reportPoint    `json:"series"`
	CategoryChanges []reportChange   `json:"category_changes"`
	Merchants       []reportMerchant `json:"merchants"`
}

// calendarDays counts local dates, including across daylight-saving transitions.
func calendarDays(from, to time.Time) int {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(t.Sub(f).Hours() / 24)
}

// prevSpan: periodo equivalente anterior a [from, end) (fechas locales): los mismos meses justo antes o un año antes,
// hasta el mismo día del mes, para comparar periodos del mismo largo.
func prevSpan(from, end time.Time, months int, compare string) (time.Time, time.Time) {
	n := months
	if compare == "year" {
		n = 12
	}
	return shiftMonths(from, -n), shiftMonths(end, -n)
}

func buildReportSummary(months []string, from, to, now time.Time, cats []store.Category, activity []store.ReportActivity, opt reportOptions) reportSummaryData {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	historyFrom := today.AddDate(0, 0, -90)
	weekFrom := today.AddDate(0, 0, 7-(int(today.Weekday())+6)%7)
	monthFrom := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, loc)
	monthEnd := monthFrom.AddDate(0, 1, 0)
	f := reportForecast{HistoryTo: today.AddDate(0, 0, -1).Format("2006-01-02"), WeekFrom: weekFrom.Format("2006-01-02"), WeekTo: weekFrom.AddDate(0, 0, 6).Format("2006-01-02"), MonthFrom: monthFrom.Format("2006-01-02"), MonthTo: monthEnd.AddDate(0, 0, -1).Format("2006-01-02"), MonthDays: calendarDays(monthFrom, monthEnd), ByCategory: []reportForecastCategory{}}
	out := reportSummaryData{From: months[0], To: months[len(months)-1], Months: []reportMonth{}, ByCategory: []reportIDTotal{}, ByAccount: []reportIDTotal{}}
	end := to.In(loc)
	if end.After(today) {
		end = today.AddDate(0, 0, 1)
	}
	out.ElapsedDays = max(0, calendarDays(from.In(loc), end))
	if opt.Compare == "" {
		opt.Compare = "previous"
	}
	shift := len(months)
	if opt.Compare == "year" {
		shift = 12
	}
	localFrom := from.In(loc)
	prevFrom, prevEnd := prevSpan(localFrom, end, len(months), opt.Compare)
	granularity, starts := buckets(localFrom, end, len(months))
	out.Filters = opt.Filter
	out.Period = reportPeriod{From: localFrom.Format("2006-01-02"), To: end.AddDate(0, 0, -1).Format("2006-01-02"), Days: out.ElapsedDays, Compare: opt.Compare, Granularity: granularity}
	if out.ElapsedDays == 0 { // periodo futuro: no hay días transcurridos
		out.Period.To = out.Period.From
	}
	out.Previous = reportPrevious{From: prevFrom.Format("2006-01-02"), To: prevEnd.AddDate(0, 0, -1).Format("2006-01-02"), Days: max(0, calendarDays(prevFrom, prevEnd)), ByCategory: []reportIDTotal{}, ByAccount: []reportIDTotal{}}
	switch first := localDate(opt.First.In(loc)); {
	case opt.First.IsZero() || !first.Before(prevEnd):
		out.Previous.Coverage = "sin_datos"
	case first.After(prevFrom.AddDate(0, 0, 3)): // empezó a registrar ya avanzado el periodo anterior
		out.Previous.Coverage = "parcial"
	default:
		out.Previous.Coverage = "completo"
	}
	out.Series = make([]reportPoint, len(starts))
	for i, st := range starts {
		last := end
		if i+1 < len(starts) {
			last = starts[i+1]
		}
		out.Series[i] = reportPoint{Start: st.Format("2006-01-02"), End: last.AddDate(0, 0, -1).Format("2006-01-02")}
	}
	prevCat, prevAcc := map[string]int64{}, map[string]int64{}
	parents := map[string]string{}
	for _, c := range cats {
		parents[c.ID] = c.ParentID
	}
	root := func(id string) string {
		for i := 0; parents[id] != "" && i < len(cats); i++ {
			id = parents[id]
		}
		return id
	}
	monthIndex := map[string]int{}
	monthly := make([]map[string]int64, len(months))
	monthlyAccount := make([]map[string]int64, len(months))
	heatFrom := today.AddDate(0, 0, -90)
	daily := map[string]int64{}
	var weekday [7]int64
	for i, m := range months {
		monthIndex[m] = i
		monthly[i] = map[string]int64{}
		monthlyAccount[i] = map[string]int64{}
		out.Months = append(out.Months, reportMonth{Month: m, ByCategory: []reportIDTotal{}, ByAccount: []reportIDTotal{}})
	}
	byCat, byAccount, history := map[string]int64{}, map[string]int64{}, map[string]int64{}
	type accCat struct{ account, category string }
	accCats, accCounts := map[accCat]int64{}, map[accCat]int{}
	out.AccountPurchases = map[string]int{}
	var firstDay time.Time
	for _, a := range activity {
		if a.OccurredAt.After(now) {
			continue
		}
		amount := a.AmountCents
		if a.Kind == "refund" {
			amount = -amount
		}
		category := root(a.CategoryID)
		if !opt.Filter.match(category, a.AccountID) {
			continue
		}
		local := a.OccurredAt.In(loc)
		day := localDate(local)
		if !day.Before(prevFrom) && day.Before(prevEnd) {
			p := &out.Previous
			p.TotalCents += amount
			prevCat[category] += amount
			prevAcc[a.AccountID] += amount
			if a.Kind == "refund" {
				p.RefundCents += a.AmountCents
			} else {
				p.PurchaseCount++
				p.PurchaseCents += amount
			}
			if i := bucketOf(starts, end, shiftMonths(day, shift)); i >= 0 {
				out.Series[i].PreviousCents += amount
			}
		}
		if i := bucketOf(starts, end, day); i >= 0 && !a.OccurredAt.Before(from) {
			out.Series[i].NetCents += amount
			if a.Kind == "refund" {
				out.Series[i].RefundCents += a.AmountCents
			} else {
				out.Series[i].PurchaseCents += amount
			}
		}
		if !a.OccurredAt.Before(from) && a.OccurredAt.Before(to) {
			if i, ok := monthIndex[local.Format("2006-01")]; ok {
				out.Months[i].SpentCents += amount
				monthly[i][category] += amount
				monthlyAccount[i][a.AccountID] += amount
			}
			weekday[(int(local.Weekday())+6)%7] += amount
			out.TotalCents += amount
			byCat[category] += amount
			byAccount[a.AccountID] += amount
			accCats[accCat{a.AccountID, category}] += amount
			if a.Kind == "refund" {
				out.RefundCents += a.AmountCents
			} else {
				accCounts[accCat{a.AccountID, category}]++
				out.AccountPurchases[a.AccountID]++
				out.PurchaseCount++
				out.PurchaseCents += amount
			}
		}
		if !local.Before(heatFrom) {
			daily[local.Format("2006-01-02")] += amount
		}
		// Only completed days train the forecast. Today's partial spending and
		// future-dated transactions must not depress or inflate the daily rate.
		if !local.Before(historyFrom) && local.Before(today) {
			history[category] += amount
			day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
			if firstDay.IsZero() || day.Before(firstDay) {
				firstDay = day
			}
		}
	}
	for id, amount := range byCat {
		if amount != 0 {
			out.ByCategory = append(out.ByCategory, reportIDTotal{id, amount})
		}
	}
	for id, amount := range byAccount {
		if amount != 0 {
			out.ByAccount = append(out.ByAccount, reportIDTotal{id, amount})
		}
	}
	out.AccountCategories = []reportAccountCategory{}
	for k, amount := range accCats {
		if amount != 0 || accCounts[k] > 0 {
			out.AccountCategories = append(out.AccountCategories, reportAccountCategory{k.account, k.category, amount, accCounts[k]})
		}
	}
	sort.Slice(out.AccountCategories, func(i, j int) bool {
		x, y := out.AccountCategories[i], out.AccountCategories[j]
		if x.AccountID != y.AccountID {
			return x.AccountID < y.AccountID
		}
		if x.SpentCents != y.SpentCents {
			return x.SpentCents > y.SpentCents
		}
		return x.CategoryID < y.CategoryID
	})
	for i := range out.Months {
		for _, c := range cats {
			if c.ParentID == "" {
				out.Months[i].ByCategory = append(out.Months[i].ByCategory, reportIDTotal{c.ID, monthly[i][c.ID]})
			}
		}
		for id, amount := range monthlyAccount[i] {
			if amount != 0 {
				out.Months[i].ByAccount = append(out.Months[i].ByAccount, reportIDTotal{id, amount})
			}
		}
		sort.Slice(out.Months[i].ByAccount, func(a, b int) bool { return out.Months[i].ByAccount[a].ID < out.Months[i].ByAccount[b].ID })
	}
	// Días de la semana: cuántos lunes, martes… hubo en el periodo transcurrido.
	var counts [7]int
	for d, n := from.In(loc), 0; d.Before(end) && n < 1200; d, n = d.AddDate(0, 0, 1), n+1 {
		counts[(int(d.Weekday())+6)%7]++
	}
	out.Weekdays = make([]reportWeekday, 7)
	for i := range out.Weekdays {
		w := reportWeekday{Weekday: i, SpentCents: weekday[i], Days: counts[i]}
		if w.Days > 0 {
			w.AvgCents = int64(math.Round(float64(w.SpentCents) / float64(w.Days)))
		}
		out.Weekdays[i] = w
	}
	out.Days = make([]reportDay, 0, 91)
	for d := heatFrom; !d.After(today); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		out.Days = append(out.Days, reportDay{Date: k, SpentCents: daily[k]})
	}
	for id, amount := range prevCat {
		if amount != 0 {
			out.Previous.ByCategory = append(out.Previous.ByCategory, reportIDTotal{id, amount})
		}
	}
	for id, amount := range prevAcc {
		if amount != 0 {
			out.Previous.ByAccount = append(out.Previous.ByAccount, reportIDTotal{id, amount})
		}
	}
	out.CategoryChanges = []reportChange{}
	for _, c := range cats {
		cur, prev := byCat[c.ID], prevCat[c.ID]
		if c.ParentID != "" || (cur == 0 && prev == 0) {
			continue
		}
		ch := reportChange{ID: c.ID, CurrentCents: cur, PreviousCents: prev, DiffCents: cur - prev}
		if out.Previous.Coverage == "completo" {
			ch.Pct = pctChange(cur, prev)
		}
		out.CategoryChanges = append(out.CategoryChanges, ch)
	}
	sort.SliceStable(out.CategoryChanges, func(i, j int) bool { return out.CategoryChanges[i].DiffCents > out.CategoryChanges[j].DiffCents })
	out.Merchants = []reportMerchant{}
	for _, list := range [][]reportIDTotal{out.ByCategory, out.ByAccount, out.Previous.ByCategory, out.Previous.ByAccount} {
		sort.Slice(list, func(i, j int) bool {
			if list[i].SpentCents == list[j].SpentCents {
				return list[i].ID < list[j].ID
			}
			return list[i].SpentCents > list[j].SpentCents
		})
	}
	if !firstDay.IsZero() {
		f.HistoryDays = calendarDays(firstDay, today)
		f.HistoryFrom = firstDay.Format("2006-01-02")
		f.Ready = f.HistoryDays >= 7
	}
	if f.Ready {
		for _, c := range cats {
			if c.ParentID != "" {
				continue
			}
			// Refund-only categories cannot imply negative future expenses.
			rate := float64(max(int64(0), history[c.ID])) / float64(f.HistoryDays)
			row := reportForecastCategory{c.ID, int64(math.Round(rate * 7)), int64(math.Round(rate * float64(f.MonthDays)))}
			f.ByCategory = append(f.ByCategory, row)
			f.WeekCents += row.WeekCents
			f.MonthCents += row.MonthCents
		}
		sort.Slice(f.ByCategory, func(i, j int) bool {
			if f.ByCategory[i].MonthCents == f.ByCategory[j].MonthCents {
				return f.ByCategory[i].ID < f.ByCategory[j].ID
			}
			return f.ByCategory[i].MonthCents > f.ByCategory[j].MonthCents
		})
	}
	out.Forecast = f
	return out
}

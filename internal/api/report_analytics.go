package api

import (
	"math"
	"sort"
	"time"

	"pulse/internal/store"
)

type reportIDTotal struct {
	ID         string `json:"id"`
	SpentCents int64  `json:"spent_cents"`
}

type reportMonth struct {
	Month      string          `json:"month"`
	SpentCents int64           `json:"spent_cents"`
	ByCategory []reportIDTotal `json:"by_category"`
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
}

// calendarDays counts local dates, including across daylight-saving transitions.
func calendarDays(from, to time.Time) int {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(t.Sub(f).Hours() / 24)
}

func buildReportSummary(months []string, from, to, now time.Time, cats []store.Category, activity []store.ReportActivity) reportSummaryData {
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
	for i, m := range months {
		monthIndex[m] = i
		monthly[i] = map[string]int64{}
		out.Months = append(out.Months, reportMonth{Month: m, ByCategory: []reportIDTotal{}})
	}
	byCat, byAccount, history := map[string]int64{}, map[string]int64{}, map[string]int64{}
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
		local := a.OccurredAt.In(loc)
		if !a.OccurredAt.Before(from) && a.OccurredAt.Before(to) {
			if i, ok := monthIndex[local.Format("2006-01")]; ok {
				out.Months[i].SpentCents += amount
				monthly[i][category] += amount
			}
			out.TotalCents += amount
			byCat[category] += amount
			byAccount[a.AccountID] += amount
			if a.Kind == "refund" {
				out.RefundCents += a.AmountCents
			} else {
				out.PurchaseCount++
				out.PurchaseCents += amount
			}
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
	for i := range out.Months {
		for _, c := range cats {
			if c.ParentID == "" {
				out.Months[i].ByCategory = append(out.Months[i].ByCategory, reportIDTotal{c.ID, monthly[i][c.ID]})
			}
		}
	}
	for _, list := range [][]reportIDTotal{out.ByCategory, out.ByAccount} {
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

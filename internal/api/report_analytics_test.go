package api

import (
	"testing"
	"time"

	"pulse/internal/store"
)

func TestReportAnalytics(t *testing.T) {
	loc, _ := time.LoadLocation("America/Guayaquil")
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, loc)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	cats := []store.Category{{ID: "food"}, {ID: "market", ParentID: "food"}, {ID: "travel"}, {ID: "empty"}}
	activity := []store.ReportActivity{
		{AccountID: "a", CategoryID: "market", Kind: "purchase", AmountCents: 14000, OccurredAt: todayAt(now, -14)},
		{AccountID: "a", CategoryID: "food", Kind: "refund", AmountCents: 1400, OccurredAt: todayAt(now, -3)},
		{AccountID: "a", CategoryID: "travel", Kind: "refund", AmountCents: 500, OccurredAt: todayAt(now, -2)},
		// Today contributes to statistics, but not to the projection.
		{AccountID: "a", CategoryID: "food", Kind: "purchase", AmountCents: 700, OccurredAt: todayAt(now, 0)},
		{AccountID: "a", CategoryID: "food", Kind: "purchase", AmountCents: 99999, OccurredAt: todayAt(now, 1)},
	}
	r := buildReportSummary([]string{"2026-09", "2026-10"}, from, from.AddDate(0, 2, 0), now, cats, activity)
	if r.TotalCents != 12800 || r.PurchaseCents != 14700 || r.RefundCents != 1900 || r.PurchaseCount != 2 || r.ElapsedDays != 44 {
		t.Fatalf("statistics: %+v", r)
	}
	if r.Months[0].SpentCents != 14000 || r.Months[1].SpentCents != -1200 || r.ByCategory[0].ID != "food" || r.ByCategory[0].SpentCents != 13300 {
		t.Fatalf("monthly and parent rollup: %+v", r)
	}
	f := r.Forecast
	if !f.Ready || f.HistoryDays != 14 || f.WeekFrom != "2026-10-19" || f.WeekTo != "2026-10-25" || f.MonthFrom != "2026-11-01" || f.MonthTo != "2026-11-30" || f.WeekCents != 6300 || f.MonthCents != 27000 {
		t.Fatalf("forecast: %+v", f)
	}
	var sumWeek, sumMonth int64
	for _, c := range f.ByCategory {
		sumWeek += c.WeekCents
		sumMonth += c.MonthCents
	}
	if sumWeek != f.WeekCents || sumMonth != f.MonthCents || len(f.ByCategory) != 3 {
		t.Fatalf("category totals: %+v", f)
	}
	// Period selection must not alter the history used to project.
	r2 := buildReportSummary([]string{"2026-10"}, from.AddDate(0, 1, 0), from.AddDate(0, 2, 0), now, cats, activity)
	if r2.Forecast.WeekCents != f.WeekCents {
		t.Fatalf("forecast changed with report range: %+v", r2.Forecast)
	}
}

func todayAt(now time.Time, offset int) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+offset, 8, 0, 0, 0, now.Location())
}

func TestForecastCalendarAndSparseHistory(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	now := time.Date(2028, 1, 31, 12, 0, 0, 0, loc) // Monday, followed by a leap February.
	cats := []store.Category{{ID: "food"}}
	from := time.Date(2028, 1, 1, 0, 0, 0, 0, loc)
	for _, days := range []int{0, 1, 6, 7, 90, 91} {
		t.Run(time.Duration(days).String(), func(t *testing.T) {
			activity := []store.ReportActivity{}
			if days > 0 {
				activity = append(activity, store.ReportActivity{CategoryID: "food", Kind: "purchase", AmountCents: 700, OccurredAt: todayAt(now, -days)})
			}
			r := buildReportSummary([]string{"2028-01"}, from, from.AddDate(0, 1, 0), now, cats, activity)
			if r.Forecast.Ready != (days >= 7 && days <= 90) {
				t.Fatalf("history=%d: %+v", days, r.Forecast)
			}
			if r.Forecast.MonthDays != 29 || r.Forecast.WeekFrom != "2028-02-07" {
				t.Fatalf("calendar: %+v", r.Forecast)
			}
		})
	}
	if days := calendarDays(time.Date(2026, 3, 1, 0, 0, 0, 0, loc), time.Date(2026, 4, 1, 0, 0, 0, 0, loc)); days != 31 {
		t.Fatalf("DST days=%d", days)
	}
}

func TestReportGroupsByUserLocalMonth(t *testing.T) {
	loc, _ := time.LoadLocation("America/Guayaquil")
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, loc)
	activity := []store.ReportActivity{{CategoryID: "food", Kind: "purchase", AmountCents: 100, OccurredAt: time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)}}
	r := buildReportSummary([]string{"2026-09", "2026-10"}, from, from.AddDate(0, 2, 0), now, []store.Category{{ID: "food"}}, activity)
	if r.Months[0].SpentCents != 100 || r.Months[1].SpentCents != 0 {
		t.Fatalf("local month: %+v", r.Months)
	}
}

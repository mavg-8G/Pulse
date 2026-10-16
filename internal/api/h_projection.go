package api

import (
	"net/http"
	"time"

	"pulse/internal/forecast"
	"pulse/internal/store"
)

// GET /api/reports/projection: proyección de fin de mes con el método que menos se equivocó (fase 13).
func (s *Server) reportProjection(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	loc := s.loc(id)
	now := s.clock.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	cats, err := s.store.Categories(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	filter, err := s.parseReportFilter(r, id, cats)
	if err != nil {
		return err
	}
	monthEnd := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, loc)
	in, err := s.projectionData(r, id, today, cats, filter, today.Format("2006-01-02"), monthEnd.Format("2006-01-02"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, forecast.Project(in))
	return nil
}

func localDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// projectionInput lleva cada movimiento a su fecha local y a su categoría raíz: las subcategorías suman al padre.
func projectionInput(today, first time.Time, cats []store.Category, txs []store.ProjectionTx, upcoming []store.UpcomingCharge) forecast.Input {
	loc := today.Location()
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
	in := forecast.Input{Today: today, Categories: []string{}}
	if !first.IsZero() {
		in.FirstDay = localDay(first, loc)
	}
	for _, c := range cats {
		if c.ParentID == "" {
			in.Categories = append(in.Categories, c.ID)
		}
	}
	for _, t := range txs {
		v := t.AmountCents
		if t.Kind == "refund" {
			v = -v
		}
		in.Txs = append(in.Txs, forecast.Tx{Category: root(t.CategoryID), Day: localDay(t.OccurredAt, loc), Cents: v,
			Purchase: t.Kind != "refund", Recurring: t.Recurring})
	}
	for _, u := range upcoming {
		d, err := time.ParseInLocation("2006-01-02", u.Date, loc)
		if err != nil {
			continue
		}
		in.Upcoming = append(in.Upcoming, forecast.Upcoming{Category: root(u.CategoryID), Day: d, Cents: u.AmountCents})
	}
	return in
}

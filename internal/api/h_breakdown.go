package api

import (
	"net/http"
	"sort"
	"strings"

	"pulse/internal/budget"
	"pulse/internal/store"
)

type breakdownMerchant struct {
	Name       string `json:"name"` // vacío: movimientos sin comercio
	Count      int    `json:"count"`
	SpentCents int64  `json:"spent_cents"`
}

type breakdownCategory struct {
	ID            string              `json:"id"`
	SpentCents    int64               `json:"spent_cents"` // compras menos devoluciones, subcategorías incluidas
	PurchaseCount int                 `json:"purchase_count"`
	RefundCents   int64               `json:"refund_cents"`
	Pending       int                 `json:"pending_review"`
	Sub           []reportIDTotal     `json:"subcategories"`
	Merchants     []breakdownMerchant `json:"merchants"`   // los que más suman, de mayor a menor
	OtherCents    int64               `json:"other_cents"` // lo que suman los comercios que no entran en la lista
	OtherCount    int                 `json:"other_count"`
}

const breakdownMerchants = 6

// GET /api/budgets/breakdown?month=AAAA-MM: en qué se gastó cada categoría principal del mes, con sus subcategorías y
// los comercios que más suman. Los comercios se descifran en el servidor solo para esta respuesta.
func (s *Server) budgetBreakdown(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	m, err := s.monthParam(r, id)
	if err != nil {
		return err
	}
	from, to, _ := budget.MonthRange(m, s.loc(id))
	cats, err := s.store.Categories(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	txs, err := s.store.Transactions(r.Context(), id.user.ID, id.seal(), store.TxFilter{From: from, To: to})
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"month": m, "categories": buildBreakdown(cats, txs)})
	return nil
}

func buildBreakdown(cats []store.Category, txs []store.Transaction) []breakdownCategory {
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
	type agg struct {
		row       breakdownCategory
		sub       map[string]int64
		merchants map[string]*breakdownMerchant
	}
	byRoot := map[string]*agg{}
	for _, t := range txs {
		rid := root(t.CategoryID)
		a := byRoot[rid]
		if a == nil {
			a = &agg{row: breakdownCategory{ID: rid}, sub: map[string]int64{}, merchants: map[string]*breakdownMerchant{}}
			byRoot[rid] = a
		}
		v := t.AmountCents
		if t.Kind == "refund" {
			v = -v
			a.row.RefundCents += t.AmountCents
		} else {
			a.row.PurchaseCount++
		}
		if t.Status == "pending_review" {
			a.row.Pending++
		}
		a.row.SpentCents += v
		if t.CategoryID != rid {
			a.sub[t.CategoryID] += v
		}
		name := strings.Join(strings.Fields(t.Merchant), " ")
		key := strings.ToLower(name)
		mm := a.merchants[key]
		if mm == nil {
			mm = &breakdownMerchant{Name: name}
			a.merchants[key] = mm
		}
		mm.Count++
		mm.SpentCents += v
	}
	out := []breakdownCategory{}
	for _, a := range byRoot {
		row := a.row
		row.Sub = []reportIDTotal{}
		for id, v := range a.sub {
			row.Sub = append(row.Sub, reportIDTotal{id, v})
		}
		sort.Slice(row.Sub, func(i, j int) bool {
			if row.Sub[i].SpentCents == row.Sub[j].SpentCents {
				return row.Sub[i].ID < row.Sub[j].ID
			}
			return row.Sub[i].SpentCents > row.Sub[j].SpentCents
		})
		list := make([]breakdownMerchant, 0, len(a.merchants))
		for _, mm := range a.merchants {
			list = append(list, *mm)
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].SpentCents == list[j].SpentCents {
				return list[i].Name < list[j].Name
			}
			return list[i].SpentCents > list[j].SpentCents
		})
		if len(list) > breakdownMerchants {
			for _, mm := range list[breakdownMerchants:] {
				row.OtherCents += mm.SpentCents
				row.OtherCount += mm.Count
			}
			list = list[:breakdownMerchants]
		}
		row.Merchants = list
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SpentCents == out[j].SpentCents {
			return out[i].ID < out[j].ID
		}
		return out[i].SpentCents > out[j].SpentCents
	})
	return out
}

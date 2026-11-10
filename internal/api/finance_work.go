package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Bound expensive read/export/import work across users, not only per address.
func (s *Server) limitFinanceWork(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		heavy := r.Method == http.MethodGet && (strings.HasPrefix(p, "/api/reports/") || p == "/api/transactions" || p == "/api/dashboard" || p == "/api/budgets/summary" || p == "/api/budgets/breakdown") || p == "/api/import/eml"
		if !heavy {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case s.financeWork <- struct{}{}:
			defer func() { <-s.financeWork }()
		default:
			w.Header().Set("Retry-After", "5")
			s.fail(w, r, errRateLimited)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExpensiveWorkIsBoundedAndRecovers(t *testing.T) {
	s := &Server{financeWork: make(chan struct{}, 2), passwordWork: make(chan struct{}, 2)}
	if !s.acquirePasswordWork() || !s.acquirePasswordWork() || s.acquirePasswordWork() {
		t.Fatal("límite PBKDF2 inválido")
	}
	s.releasePasswordWork()
	if !s.acquirePasswordWork() {
		t.Fatal("no recuperó capacidad")
	}
	entered := make(chan bool, 2)
	release := make(chan struct{})
	h := s.limitFinanceWork(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		entered <- ok && time.Until(deadline) <= 25*time.Second
		<-release
		w.WriteHeader(204)
	}))
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/reports/summary", nil))
			done <- struct{}{}
		}()
	}
	defer close(release)
	for range 2 {
		if !<-entered {
			t.Fatal("sin plazo")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/reports/summary", nil))
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("no limita la concurrencia")
	}
	// Una petición ligera no consume ni espera este presupuesto.
	light := s.limitFinanceWork(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w = httptest.NewRecorder()
	light.ServeHTTP(w, httptest.NewRequest("GET", "/api/auth/state", nil))
	if w.Code != 204 {
		t.Fatal("bloqueó verificación de sesión")
	}
}

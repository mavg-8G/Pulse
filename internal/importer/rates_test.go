package importer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFrankfurterEURUSD(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/providers/ecb/rate/eur/usd" || r.URL.Query().Get("date") != "2026-11-21" {
			t.Errorf("consulta inesperada: %s", r.URL)
		}
		fmt.Fprint(w, `{"date":"2026-11-20","base":"EUR","quote":"USD","rate":1.099}`)
	}))
	defer server.Close()
	q, err := (Frankfurter{Client: server.Client(), BaseURL: server.URL}).EURUSD(context.Background(), time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if q.Date != "2026-11-20" || q.Rate != "1.099" {
		t.Fatalf("%+v", q)
	}
	if got, err := q.Convert(20000); err != nil || got != 21980 {
		t.Fatalf("%d: %v", got, err)
	}
	if got, err := (RateQuote{Rate: "1.005"}).Convert(100); err != nil || got != 101 {
		t.Fatalf("redondeo: %d %v", got, err)
	}
	for _, rate := range []string{"0", "-1", "NaN", "1000000000"} {
		if _, err := (RateQuote{Rate: rate}).Convert(20000); err == nil {
			t.Errorf("tasa inválida %s", rate)
		}
	}
}

func TestExchangeRatesAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/latest" || r.URL.Query().Get("access_key") != "secret-key" || r.URL.Query().Get("symbols") != "USD" {
			t.Errorf("consulta inesperada: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"success":true,"date":"2026-11-20","base":"EUR","rates":{"USD":1.10}}`)
	}))
	defer server.Close()
	q, err := (ExchangeRatesAPI{Key: "secret-key", Client: server.Client(), BaseURL: server.URL}).EURUSD(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if q.Source != "Exchange Rates API" || q.Date != "2026-11-20" {
		t.Fatalf("%+v", q)
	}
	if got, err := q.Convert(20000); err != nil || got != 22000 {
		t.Fatalf("EUR a USD: %d %v", got, err)
	}
	if got, err := q.ConvertInverse(22000); err != nil || got != 20000 {
		t.Fatalf("USD a EUR: %d %v", got, err)
	}
}

func TestExchangeRatesAPIFailClosed(t *testing.T) {
	for _, body := range []string{`{"success":false,"error":{"code":101,"info":"secret-key"}}`, `{"success":true,"date":"2026-11-20","base":"USD","rates":{"USD":1.1}}`, `{"success":true,"date":"2026-11-20","base":"EUR","rates":{}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		_, err := (ExchangeRatesAPI{Key: "secret-key", Client: server.Client(), BaseURL: server.URL}).EURUSD(context.Background(), time.Time{})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "secret-key") {
			t.Fatalf("error inseguro o ausente: %v", err)
		}
	}
	_, err := (ExchangeRatesAPI{Key: "secret-key", BaseURL: "http://127.0.0.1:1"}).EURUSD(context.Background(), time.Time{})
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("error de red inseguro: %v", err)
	}
}

func TestFrankfurterRejectsInvalidRates(t *testing.T) {
	for _, body := range []string{
		`{"date":"2026-11-22","base":"EUR","quote":"USD","rate":1.1}`,
		`{"date":"2026-11-01","base":"EUR","quote":"USD","rate":1.1}`,
		`{"date":"2026-11-20","base":"USD","quote":"EUR","rate":1.1}`,
		`{"date":"2026-11-20","base":"EUR","quote":"USD","rate":0}`,
		`not json`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		_, err := (Frankfurter{Client: server.Client(), BaseURL: server.URL}).EURUSD(context.Background(), time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC))
		server.Close()
		if err == nil {
			t.Errorf("aceptó %s", body)
		}
	}
}

package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

var decimalRate = regexp.MustCompile(`^[0-9]{1,8}(?:\.[0-9]{1,12})?$`)

// RateProvider devuelve una tasa histórica de referencia, no la liquidación del banco.
type RateProvider interface {
	EURUSD(context.Context, time.Time) (RateQuote, error)
}

type RateQuote struct {
	Date   string
	Rate   string
	Source string
}

// Convert redondea a centavos sin operaciones monetarias en punto flotante.
func (q RateQuote) Convert(cents int64) (int64, error) {
	return q.convert(cents, false)
}

func (q RateQuote) ConvertInverse(cents int64) (int64, error) {
	return q.convert(cents, true)
}

func (q RateQuote) convert(cents int64, inverse bool) (int64, error) {
	if !decimalRate.MatchString(q.Rate) || cents <= 0 {
		return 0, errors.New("tasa de cambio inválida")
	}
	rate, ok := new(big.Rat).SetString(q.Rate)
	if !ok || rate.Sign() <= 0 {
		return 0, errors.New("tasa de cambio inválida")
	}
	if inverse {
		rate.Inv(rate)
	}
	n := new(big.Int).Mul(big.NewInt(cents), rate.Num())
	n.Add(n, new(big.Int).Quo(rate.Denom(), big.NewInt(2)))
	n.Quo(n, rate.Denom())
	if !n.IsInt64() || n.Int64() <= 0 || n.Int64() > 100_000_000 {
		return 0, errors.New("monto convertido fuera de los límites permitidos")
	}
	return n.Int64(), nil
}

// Frankfurter solo recibe par y fecha; no recibe datos del correo ni del usuario.
type Frankfurter struct {
	Client  *http.Client
	BaseURL string
}

func (f Frankfurter) EURUSD(ctx context.Context, when time.Time) (RateQuote, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	base := f.BaseURL
	if base == "" {
		base = "https://api.frankfurter.dev"
	}
	date := when.Format("2006-01-02")
	u := base + "/v2/providers/ecb/rate/eur/usd?date=" + url.QueryEscape(date)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return RateQuote{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return RateQuote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RateQuote{}, fmt.Errorf("servicio de tasas: HTTP %d", resp.StatusCode)
	}
	var row struct {
		Date  string      `json:"date"`
		Base  string      `json:"base"`
		Quote string      `json:"quote"`
		Rate  json.Number `json:"rate"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&row); err != nil {
		return RateQuote{}, errors.New("respuesta de tasa inválida")
	}
	d, err := time.Parse("2006-01-02", row.Date)
	want, _ := time.Parse("2006-01-02", date)
	if err != nil || d.After(want) || want.Sub(d) > 7*24*time.Hour || row.Base != "EUR" || row.Quote != "USD" {
		return RateQuote{}, errors.New("tasa histórica no disponible")
	}
	q := RateQuote{Date: row.Date, Rate: row.Rate.String(), Source: "BCE vía Frankfurter"}
	if _, err := q.Convert(100); err != nil {
		return RateQuote{}, err
	}
	return q, nil
}

// ExchangeRatesAPI consulta el último tipo de cambio; el proveedor siempre usa EUR de base.
type ExchangeRatesAPI struct {
	Key     string
	Client  *http.Client
	BaseURL string
}

func (e ExchangeRatesAPI) EURUSD(ctx context.Context, _ time.Time) (RateQuote, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	base := e.BaseURL
	if base == "" {
		base = "https://api.exchangeratesapi.io"
	}
	u, err := url.Parse(base + "/v1/latest")
	if err != nil {
		return RateQuote{}, errors.New("servicio de cambio no disponible")
	}
	values := url.Values{"access_key": {e.Key}, "symbols": {"USD"}}
	u.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return RateQuote{}, errors.New("servicio de cambio no disponible")
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if e.Client != nil {
		client = *e.Client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	resp, err := client.Do(req)
	// Los errores de transporte contienen la URL con la clave. Nunca devolverlos ni registrarlos.
	if err != nil {
		return RateQuote{}, errors.New("servicio de cambio no disponible")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return RateQuote{}, fmt.Errorf("servicio de cambio: HTTP %d", resp.StatusCode)
	}
	var row struct {
		Success bool                   `json:"success"`
		Base    string                 `json:"base"`
		Date    string                 `json:"date"`
		Rates   map[string]json.Number `json:"rates"`
		Error   struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&row); err != nil {
		return RateQuote{}, errors.New("respuesta de cambio inválida")
	}
	if !row.Success {
		return RateQuote{}, fmt.Errorf("servicio de cambio rechazó la consulta (código %d)", row.Error.Code)
	}
	if row.Base != "EUR" {
		return RateQuote{}, errors.New("moneda base de la tasa inválida")
	}
	if _, err := time.Parse("2006-01-02", row.Date); err != nil {
		return RateQuote{}, errors.New("fecha de tasa inválida")
	}
	q := RateQuote{Date: row.Date, Rate: row.Rates["USD"].String(), Source: exchangeSource}
	if _, err := q.Convert(100); err != nil {
		return RateQuote{}, err
	}
	return q, nil
}

package importer

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/store"
)

type rateLookup func(context.Context, time.Time) (RateQuote, error)

func (f rateLookup) EURUSD(ctx context.Context, when time.Time) (RateQuote, error) {
	return f(ctx, when)
}

func TestExchangeCacheSharesRateAndLimitsAttempts(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "rates.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	c := clock.NewFake(start)
	keys, err := crypto.NewKeyring(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	st := &store.Store{DB: database, Clock: c}
	first := &Importer{Store: st, Keys: keys, Clock: c}
	second := &Importer{Store: st, Keys: keys, Clock: c}
	var calls atomic.Int32
	success := rateLookup(func(context.Context, time.Time) (RateQuote, error) {
		calls.Add(1)
		return RateQuote{Rate: "1.10", Date: "2026-11-20", Source: exchangeSource}, nil
	})
	var workers sync.WaitGroup
	for _, im := range []*Importer{first, second} {
		workers.Go(func() {
			quote, err := im.cachedExchangeRate(ctx, "key", success)
			if err != nil || quote.Rate != "1.10" {
				t.Errorf("quote: %+v, %v", quote, err)
			}
		})
	}
	workers.Wait()
	c.Set(start.Add(8*time.Hour - time.Millisecond))
	if _, err := second.cachedExchangeRate(ctx, "key", success); err != nil || calls.Load() != 1 {
		t.Fatalf("la tasa debe seguir vigente: consultas=%d, error=%v", calls.Load(), err)
	}
	failed := rateLookup(func(context.Context, time.Time) (RateQuote, error) {
		calls.Add(1)
		return RateQuote{}, errors.New("servicio no disponible")
	})
	for _, elapsed := range []time.Duration{8 * time.Hour, 16 * time.Hour, 23 * time.Hour} {
		c.Set(start.Add(elapsed))
		if _, err := first.cachedExchangeRate(ctx, "key", failed); err == nil {
			t.Fatalf("se usó una tasa vencida después de %s", elapsed)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("se superó el límite de consultas: %d", calls.Load())
	}
	if _, err := second.cachedExchangeRate(ctx, "different-key", success); err != nil || calls.Load() != 4 {
		t.Fatalf("otra clave debe tener su propia cuota: consultas=%d, error=%v", calls.Load(), err)
	}
	c.Set(start.Add(24 * time.Hour))
	if _, err := second.cachedExchangeRate(ctx, "key", success); err != nil || calls.Load() != 5 {
		t.Fatalf("se esperaba un nuevo intento: consultas=%d, error=%v", calls.Load(), err)
	}
}

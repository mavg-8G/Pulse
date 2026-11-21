package importer

import (
	"context"
	"errors"
	"time"
)

const exchangeRefreshInterval = 8 * time.Hour
const exchangeSource = "Exchange Rates API"

func (im *Importer) cachedExchangeRate(ctx context.Context, key string, provider RateProvider) (RateQuote, error) {
	hash := im.Keys.Lookup("exchange.rate-cache", []byte(key))
	// Other importers may be fetching the same key. Give their bounded HTTP
	// request time to finish, without holding a database lock.
	wait, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	for {
		now := im.Clock.Now()
		cached, err := im.Store.CachedExchangeRate(wait, hash)
		if err != nil {
			return RateQuote{}, err
		}
		if cached.ExpiresAt > now.UnixMilli() {
			return RateQuote{Date: cached.Date, Rate: cached.Rate, Source: exchangeSource}, nil
		}
		if cached.AttemptedAt == 0 || now.UnixMilli()-cached.AttemptedAt >= exchangeRefreshInterval.Milliseconds() {
			reserved, err := im.Store.ReserveExchangeRate(wait, hash, now, exchangeRefreshInterval)
			if err != nil {
				return RateQuote{}, err
			}
			if reserved {
				return im.refreshExchangeRate(ctx, hash, now, provider)
			}
			continue
		}
		if cached.Finished || now.UnixMilli()-cached.AttemptedAt >= (12*time.Second).Milliseconds() {
			return RateQuote{}, errors.New("no hay una tasa vigente y la próxima consulta aún no está disponible")
		}
		select {
		case <-wait.Done():
			return RateQuote{}, wait.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (im *Importer) refreshExchangeRate(ctx context.Context, hash []byte, attempt time.Time, provider RateProvider) (RateQuote, error) {
	quote, err := provider.EURUSD(ctx, attempt)
	var expiresAt int64
	if err == nil {
		expiresAt = im.Clock.Now().Add(exchangeRefreshInterval).UnixMilli()
	} else {
		quote = RateQuote{}
	}
	// A canceled importer still counts toward the quota and must release waiters.
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if saveErr := im.Store.FinishExchangeRate(save, hash, attempt, quote.Rate, quote.Date, expiresAt); saveErr != nil {
		return RateQuote{}, saveErr
	}
	return quote, err
}

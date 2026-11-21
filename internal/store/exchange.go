package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) ExchangeKey(ctx context.Context, userID string) ([]byte, error) {
	var key []byte
	err := s.DB.QueryRowContext(ctx, `SELECT api_key_enc FROM exchange_settings WHERE user_id = ?`, userID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return key, err
}

func (s *Store) SetExchangeKey(ctx context.Context, userID string, key []byte) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO exchange_settings (user_id, api_key_enc) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET api_key_enc=excluded.api_key_enc`, userID, key)
	return err
}

func (s *Store) DeleteExchangeKey(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM exchange_settings WHERE user_id = ?`, userID)
	return err
}

type ExchangeRateCache struct {
	AttemptedAt int64
	Finished    bool
	Rate        string
	Date        string
	ExpiresAt   int64
}

func (s *Store) CachedExchangeRate(ctx context.Context, hash []byte) (ExchangeRateCache, error) {
	var c ExchangeRateCache
	err := s.DB.QueryRowContext(ctx, `SELECT attempted_at, finished, rate, quote_date, expires_at FROM exchange_rate_cache WHERE key_hash = ?`, hash).
		Scan(&c.AttemptedAt, &c.Finished, &c.Rate, &c.Date, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	return c, err
}

// ReserveExchangeRate atomically records the attempt before any request goes out.
func (s *Store) ReserveExchangeRate(ctx context.Context, hash []byte, now time.Time, interval time.Duration) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO exchange_rate_cache (key_hash, attempted_at) VALUES (?, ?)
		ON CONFLICT(key_hash) DO UPDATE SET attempted_at = excluded.attempted_at, finished = 0
		WHERE attempted_at <= ? AND expires_at <= excluded.attempted_at`, hash, now.UnixMilli(), now.Add(-interval).UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) FinishExchangeRate(ctx context.Context, hash []byte, attempt time.Time, rate, date string, expiresAt int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE exchange_rate_cache SET finished = 1, rate = ?, quote_date = ?, expires_at = ?
		WHERE key_hash = ? AND attempted_at = ?`, rate, date, expiresAt, hash, attempt.UnixMilli())
	return err
}

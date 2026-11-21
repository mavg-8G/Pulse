package store

import (
	"context"
	"database/sql"
	"errors"
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

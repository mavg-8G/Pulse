package store

import (
	"context"
	"database/sql"
	"errors"
)

// Setting devuelve un ajuste del servidor o ErrNotFound.
func (s *Store) Setting(ctx context.Context, key string) ([]byte, error) {
	var v []byte
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM server_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key string, value []byte) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO server_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, s.now())
	return err
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM server_settings WHERE key = ?`, key)
	return err
}

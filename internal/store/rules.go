package store

import (
	"context"
	"time"
)

type MerchantRule struct {
	ID         string
	PatternEnc []byte
	CategoryID string
	CreatedAt  time.Time
}

// UpsertRule crea la regla o, si el patrón ya existía, cambia su categoría.
func (s *Store) UpsertRule(ctx context.Context, userID, id string, patternEnc, patternHash []byte, categoryID string) (string, error) {
	var out string
	err := s.DB.QueryRowContext(ctx, `INSERT INTO merchant_rules (id, user_id, pattern_enc, pattern_hash, category_id, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, pattern_hash) DO UPDATE SET category_id = excluded.category_id RETURNING id`,
		id, userID, patternEnc, patternHash, categoryID, s.now()).Scan(&out)
	if isFK(err) {
		return "", ErrNotFound
	}
	return out, err
}

func (s *Store) Rules(ctx context.Context, userID string) ([]MerchantRule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, pattern_enc, category_id, created_at FROM merchant_rules WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MerchantRule
	for rows.Next() {
		var r MerchantRule
		var c string
		if err := rows.Scan(&r.ID, &r.PatternEnc, &r.CategoryID, &c); err != nil {
			return nil, err
		}
		r.CreatedAt = ParseTS(c)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRule(ctx context.Context, userID, id string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM merchant_rules WHERE id = ? AND user_id = ?`, id, userID))
}

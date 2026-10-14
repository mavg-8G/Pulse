package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Account struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Institution string    `json:"institution"`
	CardSuffix  string    `json:"card_suffix"`
	Currency    string    `json:"currency"`
	Color       string    `json:"color"`
	Icon        string    `json:"icon"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const accCols = `id, name, type, institution, card_suffix, currency, color, icon, archived, created_at, updated_at`

func scanAccount(row interface{ Scan(...any) error }) (*Account, error) {
	var a Account
	var arch int
	var c, u string
	err := row.Scan(&a.ID, &a.Name, &a.Type, &a.Institution, &a.CardSuffix, &a.Currency, &a.Color, &a.Icon, &arch, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	a.Archived, a.CreatedAt, a.UpdatedAt = arch == 1, ParseTS(c), ParseTS(u)
	return &a, err
}

func (s *Store) Accounts(ctx context.Context, userID string, includeArchived bool) ([]Account, error) {
	q := `SELECT ` + accCols + ` FROM accounts WHERE user_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY archived, name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) Account(ctx context.Context, userID, id string) (*Account, error) {
	return scanAccount(s.DB.QueryRowContext(ctx, `SELECT `+accCols+` FROM accounts WHERE user_id = ? AND id = ?`, userID, id))
}

func insertAccount(ctx context.Context, ex execer, userID string, a *Account) error {
	ts := TS(a.CreatedAt)
	_, err := ex.ExecContext(ctx, `INSERT INTO accounts (id, user_id, name, type, institution, card_suffix, currency, color, icon, archived, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, userID, a.Name, a.Type, a.Institution, a.CardSuffix, a.Currency, a.Color, a.Icon, boolInt(a.Archived), ts, ts)
	return err
}

func (s *Store) CreateAccount(ctx context.Context, userID string, a *Account) error {
	a.CreatedAt = s.Clock.Now()
	a.UpdatedAt = a.CreatedAt
	return insertAccount(ctx, s.DB, userID, a)
}

func (s *Store) CreateAccountTx(ctx context.Context, tx *sql.Tx, userID string, a *Account) error {
	a.CreatedAt = s.Clock.Now()
	a.UpdatedAt = a.CreatedAt
	return insertAccount(ctx, tx, userID, a)
}

func (s *Store) UpdateAccount(ctx context.Context, userID string, a *Account) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE accounts SET name = ?, type = ?, institution = ?, card_suffix = ?, currency = ?, color = ?, icon = ?, archived = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, a.Name, a.Type, a.Institution, a.CardSuffix, a.Currency, a.Color, a.Icon, boolInt(a.Archived), s.now(), a.ID, userID))
}

// DeleteAccount solo borra cuentas sin movimientos; las demás se archivan.
func (s *Store) DeleteAccount(ctx context.Context, userID, id string) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions WHERE user_id = ? AND account_id = ?`, userID, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return Conflict("la cuenta tiene movimientos: archívala en lugar de borrarla")
	}
	return affected(s.DB.ExecContext(ctx, `DELETE FROM accounts WHERE id = ? AND user_id = ?`, id, userID))
}

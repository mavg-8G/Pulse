package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Budget struct {
	ID          string    `json:"id"`
	CategoryID  string    `json:"category_id"`
	AmountCents int64     `json:"amount_cents"`
	Thresholds  string    `json:"thresholds"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const budCols = `id, category_id, amount_cents, thresholds, active, created_at, updated_at`

func scanBudget(row interface{ Scan(...any) error }) (*Budget, error) {
	var b Budget
	var act int
	var c, u string
	err := row.Scan(&b.ID, &b.CategoryID, &b.AmountCents, &b.Thresholds, &act, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	b.Active, b.CreatedAt, b.UpdatedAt = act == 1, ParseTS(c), ParseTS(u)
	return &b, err
}

func (s *Store) Budgets(ctx context.Context, userID string) ([]Budget, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+budCols+` FROM budgets WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Budget{}
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Store) Budget(ctx context.Context, userID, id string) (*Budget, error) {
	return scanBudget(s.DB.QueryRowContext(ctx, `SELECT `+budCols+` FROM budgets WHERE user_id = ? AND id = ?`, userID, id))
}

func (s *Store) CreateBudget(ctx context.Context, userID string, b *Budget) error {
	now := s.Clock.Now()
	b.CreatedAt, b.UpdatedAt = now, now
	_, err := s.DB.ExecContext(ctx, `INSERT INTO budgets (id, user_id, category_id, amount_cents, thresholds, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, b.ID, userID, b.CategoryID, b.AmountCents, b.Thresholds, boolInt(b.Active), TS(now), TS(now))
	if isUnique(err) {
		return Conflict("esa categoría ya tiene presupuesto")
	}
	if isFK(err) {
		return ErrNotFound
	}
	return err
}

func (s *Store) UpdateBudget(ctx context.Context, userID string, b *Budget) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE budgets SET amount_cents = ?, thresholds = ?, active = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		b.AmountCents, b.Thresholds, boolInt(b.Active), s.now(), b.ID, userID))
}

func (s *Store) DeleteBudget(ctx context.Context, userID, id string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM budgets WHERE id = ? AND user_id = ?`, id, userID))
}

// InsertAlerts registra los umbrales cruzados; los ya registrados ese mes se ignoran
// gracias a la clave primaria (budget_id, month, threshold). Devuelve los nuevos.
func (s *Store) InsertAlerts(ctx context.Context, budgetID, month string, thresholds []int, spent int64) ([]int, error) {
	var created []int
	for _, t := range thresholds {
		res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO budget_alerts (budget_id, month, threshold, spent_cents, created_at) VALUES (?, ?, ?, ?, ?)`,
			budgetID, month, t, spent, s.now())
		if err != nil {
			return created, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			created = append(created, t)
		}
	}
	return created, nil
}

type Alert struct {
	BudgetID   string    `json:"budget_id"`
	CategoryID string    `json:"category_id"`
	Month      string    `json:"month"`
	Threshold  int       `json:"threshold"`
	SpentCents int64     `json:"spent_cents"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) Alerts(ctx context.Context, userID, month string) ([]Alert, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.budget_id, b.category_id, a.month, a.threshold, a.spent_cents, a.created_at
		FROM budget_alerts a JOIN budgets b ON b.id = a.budget_id WHERE b.user_id = ? AND a.month = ? ORDER BY a.created_at DESC, a.threshold DESC`, userID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		var c string
		if err := rows.Scan(&a.BudgetID, &a.CategoryID, &a.Month, &a.Threshold, &a.SpentCents, &c); err != nil {
			return nil, err
		}
		a.CreatedAt = ParseTS(c)
		out = append(out, a)
	}
	return out, rows.Err()
}

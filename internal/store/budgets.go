package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Budget es un tope mensual por categoría (con sus subcategorías) o por cuenta: exactamente uno de los dos.
type Budget struct {
	ID           string           `json:"id"`
	CategoryID   string           `json:"category_id"`
	AccountID    string           `json:"account_id"`
	AmountCents  int64            `json:"amount_cents"`
	Thresholds   string           `json:"thresholds"`
	Active       bool             `json:"active"`
	Rollover     bool             `json:"rollover"`
	RolloverFrom string           `json:"rollover_from"`
	Months       map[string]int64 `json:"months"` // topes de meses concretos (AAAA-MM → centavos)
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

const budCols = `id, category_id, account_id, amount_cents, thresholds, active, rollover, rollover_from, created_at, updated_at`

func scanBudget(row interface{ Scan(...any) error }) (*Budget, error) {
	var b Budget
	var act, roll int
	var cat, acc sql.NullString
	var c, u string
	err := row.Scan(&b.ID, &cat, &acc, &b.AmountCents, &b.Thresholds, &act, &roll, &b.RolloverFrom, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	b.CategoryID, b.AccountID = cat.String, acc.String
	b.Active, b.Rollover, b.CreatedAt, b.UpdatedAt = act == 1, roll == 1, ParseTS(c), ParseTS(u)
	b.Months = map[string]int64{}
	return &b, err
}

func (s *Store) Budgets(ctx context.Context, userID string) ([]Budget, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+budCols+` FROM budgets WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	out := []Budget{}
	idx := map[string]int{}
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		idx[b.ID] = len(out)
		out = append(out, *b)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT m.budget_id, m.month, m.amount_cents FROM budget_months m
		JOIN budgets b ON b.id = m.budget_id WHERE b.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, month string
		var v int64
		if err := rows.Scan(&id, &month, &v); err != nil {
			return nil, err
		}
		if i, ok := idx[id]; ok {
			out[i].Months[month] = v
		}
	}
	return out, rows.Err()
}

func (s *Store) Budget(ctx context.Context, userID, id string) (*Budget, error) {
	b, err := scanBudget(s.DB.QueryRowContext(ctx, `SELECT `+budCols+` FROM budgets WHERE user_id = ? AND id = ?`, userID, id))
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT month, amount_cents FROM budget_months WHERE budget_id = ?`, b.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var month string
		var v int64
		if err := rows.Scan(&month, &v); err != nil {
			return nil, err
		}
		b.Months[month] = v
	}
	return b, rows.Err()
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *Store) CreateBudget(ctx context.Context, userID string, b *Budget) error {
	now := s.Clock.Now()
	b.CreatedAt, b.UpdatedAt = now, now
	if b.Months == nil {
		b.Months = map[string]int64{}
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO budgets (id, user_id, category_id, account_id, amount_cents, thresholds, active, rollover, rollover_from, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, b.ID, userID, nullable(b.CategoryID), nullable(b.AccountID), b.AmountCents, b.Thresholds,
		boolInt(b.Active), boolInt(b.Rollover), b.RolloverFrom, TS(now), TS(now))
	if isUnique(err) {
		if b.AccountID != "" {
			return Conflict("esa cuenta ya tiene presupuesto")
		}
		return Conflict("esa categoría ya tiene presupuesto")
	}
	if isFK(err) {
		return ErrNotFound
	}
	return err
}

func (s *Store) UpdateBudget(ctx context.Context, userID string, b *Budget) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE budgets SET amount_cents = ?, thresholds = ?, active = ?, rollover = ?, rollover_from = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, b.AmountCents, b.Thresholds, boolInt(b.Active), boolInt(b.Rollover), b.RolloverFrom, s.now(), b.ID, userID))
}

func (s *Store) DeleteBudget(ctx context.Context, userID, id string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM budgets WHERE id = ? AND user_id = ?`, id, userID))
}

// SetBudgetMonth fija (amount > 0) o quita (amount = 0) el tope de un mes concreto. Comprueba el dueño.
func (s *Store) SetBudgetMonth(ctx context.Context, userID, budgetID, month string, amount int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM budgets WHERE id = ? AND user_id = ?`, budgetID, userID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var err error
		if amount <= 0 {
			_, err = tx.ExecContext(ctx, `DELETE FROM budget_months WHERE budget_id = ? AND month = ?`, budgetID, month)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO budget_months (budget_id, month, amount_cents) VALUES (?, ?, ?)
				ON CONFLICT (budget_id, month) DO UPDATE SET amount_cents = excluded.amount_cents`, budgetID, month, amount)
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE budgets SET updated_at = ? WHERE id = ?`, s.now(), budgetID)
		return err
	})
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
	AccountID  string    `json:"account_id"`
	Month      string    `json:"month"`
	Threshold  int       `json:"threshold"`
	SpentCents int64     `json:"spent_cents"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) Alerts(ctx context.Context, userID, month string) ([]Alert, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.budget_id, COALESCE(b.category_id, ''), COALESCE(b.account_id, ''), a.month, a.threshold, a.spent_cents, a.created_at
		FROM budget_alerts a JOIN budgets b ON b.id = a.budget_id WHERE b.user_id = ? AND a.month = ? ORDER BY a.created_at DESC, a.threshold DESC`, userID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		var c string
		if err := rows.Scan(&a.BudgetID, &a.CategoryID, &a.AccountID, &a.Month, &a.Threshold, &a.SpentCents, &c); err != nil {
			return nil, err
		}
		a.CreatedAt = ParseTS(c)
		out = append(out, a)
	}
	return out, rows.Err()
}

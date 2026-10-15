package store

import (
	"context"
	"time"

	"pulse/internal/auth"
	"pulse/internal/crypto"
)

// ---------- precisión del modo sombra ----------

// SetReview anota el resultado de la primera revisión de un gasto importado ("ok" o "fixed").
func (s *Store) SetReview(ctx context.Context, userID, txID, review string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE transactions SET review = ? WHERE id = ? AND user_id = ? AND review = '' AND source != 'manual'`,
		review, txID, userID)
	return err
}

type ParserAccuracy struct {
	Parser   string `json:"parser"`
	Reviewed int    `json:"reviewed"`
	Correct  int    `json:"correct"`
}

// MinReviews es cuántas revisiones hacen falta antes de confiar en un banco.
const MinReviews = 20

// Trusted: con suficientes revisiones, al menos el 98 % salió bien sin corregir.
func (p ParserAccuracy) Trusted() bool {
	return p.Reviewed >= MinReviews && p.Correct*100 >= p.Reviewed*98
}

func (s *Store) ParserAccuracy(ctx context.Context, userID string) ([]ParserAccuracy, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT parser, COUNT(*), SUM(review = 'ok') FROM transactions
		WHERE user_id = ? AND review != '' AND parser != '' GROUP BY parser ORDER BY parser`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParserAccuracy{}
	for rows.Next() {
		var p ParserAccuracy
		if err := rows.Scan(&p.Parser, &p.Reviewed, &p.Correct); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------- detector de plantillas cambiadas ----------

type TemplateAlert struct {
	Parser    string    `json:"parser"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}

// AddTemplateAlert registra un correo no entendido; false si ese mensaje ya estaba registrado.
func (s *Store) AddTemplateAlert(ctx context.Context, userID string, sl *crypto.Sealer, parser, subject string, msgHash []byte) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO template_alerts (id, user_id, parser, subject_enc, msg_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (user_id, msg_hash) DO NOTHING`,
		auth.NewID(), userID, parser, sl.Seal("template.subject", subject), msgHash, TS(s.Clock.Now()))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) TemplateAlerts(ctx context.Context, userID string, sl *crypto.Sealer, since time.Time) ([]TemplateAlert, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT parser, subject_enc, created_at FROM template_alerts
		WHERE user_id = ? AND created_at >= ? ORDER BY created_at DESC, id`, userID, TS(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TemplateAlert{}
	for rows.Next() {
		var a TemplateAlert
		var subj []byte
		var c string
		if err := rows.Scan(&a.Parser, &subj, &c); err != nil {
			return nil, err
		}
		if a.Subject, err = sl.Open("template.subject", subj); err != nil {
			return nil, err
		}
		a.CreatedAt = ParseTS(c)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) HasTemplateAlert(ctx context.Context, userID string, msgHash []byte) (bool, error) {
	var found bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM template_alerts WHERE user_id = ? AND msg_hash = ?)`, userID, msgHash).Scan(&found)
	return found, err
}

// ResolveTemplateAlert removes only this user's alert after the message has been
// successfully imported, identified as a duplicate, or classified as non-expense.
func (s *Store) ResolveTemplateAlert(ctx context.Context, userID string, msgHash []byte) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM template_alerts WHERE user_id = ? AND msg_hash = ?`, userID, msgHash)
	return err
}

// ---------- reportes y conciliación ----------

// ReportActivity reads only the fields needed for analytics, without decrypting
// merchants or loading transaction descriptions into memory.
type ReportActivity struct {
	AccountID, CategoryID, Kind string
	AmountCents                 int64
	OccurredAt                  time.Time
}

func (s *Store) ReportActivity(ctx context.Context, userID string, from, to, historyFrom, now time.Time) ([]ReportActivity, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT account_id, category_id, kind, amount_cents, occurred_at
		FROM transactions WHERE user_id = ? AND occurred_at <= ?
		AND ((occurred_at >= ? AND occurred_at < ?) OR occurred_at >= ?) ORDER BY occurred_at`, userID, TS(now), TS(from), TS(to), TS(historyFrom))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportActivity{}
	for rows.Next() {
		var a ReportActivity
		var occurred string
		if err := rows.Scan(&a.AccountID, &a.CategoryID, &a.Kind, &a.AmountCents, &occurred); err != nil {
			return nil, err
		}
		a.OccurredAt = ParseTS(occurred)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SpentByAccount suma compras menos reversos por cuenta en [from, to).
func (s *Store) SpentByAccount(ctx context.Context, userID string, from, to time.Time) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT account_id, SUM(CASE kind WHEN 'refund' THEN -amount_cents ELSE amount_cents END)
		FROM transactions WHERE user_id = ? AND occurred_at >= ? AND occurred_at < ? GROUP BY account_id`, userID, TS(from), TS(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var v int64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

func (s *Store) SetStatement(ctx context.Context, userID, accountID, month string, totalCents int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO statements (user_id, account_id, month, total_cents, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, account_id, month) DO UPDATE SET total_cents = excluded.total_cents, updated_at = excluded.updated_at`,
		userID, accountID, month, totalCents, TS(s.Clock.Now()))
	if isFK(err) {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteStatement(ctx context.Context, userID, accountID, month string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM statements WHERE user_id = ? AND account_id = ? AND month = ?`, userID, accountID, month)
	return err
}

// Statements devuelve el total ingresado por cuenta para el mes.
func (s *Store) Statements(ctx context.Context, userID, month string) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT account_id, total_cents FROM statements WHERE user_id = ? AND month = ?`, userID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var v int64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// ---------- gastos recurrentes ----------

type Recurring struct {
	ID          string `json:"id"`
	AccountID   string `json:"account_id"`
	CategoryID  string `json:"category_id"`
	AmountCents int64  `json:"amount_cents"`
	Merchant    string `json:"merchant"`
	Day         int    `json:"day"`
	NextDate    string `json:"next_date"` // AAAA-MM-DD, zona del usuario
}

func (s *Store) CreateRecurring(ctx context.Context, userID string, sl *crypto.Sealer, r *Recurring) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO recurring (id, user_id, account_id, category_id, amount_cents, merchant_enc, day, next_date, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, userID, r.AccountID, r.CategoryID, r.AmountCents, sl.Seal("recurring.merchant", r.Merchant),
		r.Day, r.NextDate, TS(s.Clock.Now()))
	if isFK(err) {
		return ErrNotFound
	}
	return err
}

// Recurrings lista los del usuario; con due != "" solo los que vencen ese día o antes.
func (s *Store) Recurrings(ctx context.Context, userID string, sl *crypto.Sealer, due string) ([]Recurring, error) {
	q := `SELECT id, account_id, category_id, amount_cents, merchant_enc, day, next_date FROM recurring WHERE user_id = ?`
	args := []any{userID}
	if due != "" {
		q += ` AND next_date <= ?`
		args = append(args, due)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY day, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recurring{}
	for rows.Next() {
		var r Recurring
		var m []byte
		if err := rows.Scan(&r.ID, &r.AccountID, &r.CategoryID, &r.AmountCents, &m, &r.Day, &r.NextDate); err != nil {
			return nil, err
		}
		if r.Merchant, err = sl.Open("recurring.merchant", m); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AdvanceRecurring(ctx context.Context, userID, id, next string) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE recurring SET next_date = ? WHERE id = ? AND user_id = ?`, next, id, userID))
}

func (s *Store) DeleteRecurring(ctx context.Context, userID, id string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM recurring WHERE id = ? AND user_id = ?`, id, userID))
}

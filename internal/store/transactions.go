package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"pulse/internal/crypto"
)

type Transaction struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"account_id"`
	CategoryID  string    `json:"category_id"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	OccurredAt  time.Time `json:"occurred_at"`
	Kind        string    `json:"kind"`
	Merchant    string    `json:"merchant"`
	Description string    `json:"description"`
	Notes       string    `json:"notes"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Version sube con cada cambio: la cola sin conexión la envía en If-Match para detectar conflictos.
	Version int64  `json:"version"`
	LastOp  string `json:"-"`
	// Solo para importados:
	MessageHash []byte   `json:"-"`
	Parser      string   `json:"parser,omitempty"`
	Confidence  *float64 `json:"confidence,omitempty"`
}

var ErrDuplicate = Conflict("ese correo ya fue importado")

const txCols = `id, account_id, category_id, amount_cents, currency, occurred_at, kind, merchant_enc, description_enc, notes_enc, source, status, created_at, updated_at, version, last_op`

func scanTx(row interface{ Scan(...any) error }, s *crypto.Sealer) (*Transaction, error) {
	var t Transaction
	var occ, c, u string
	var m, d, n []byte
	err := row.Scan(&t.ID, &t.AccountID, &t.CategoryID, &t.AmountCents, &t.Currency, &occ, &t.Kind, &m, &d, &n, &t.Source, &t.Status, &c, &u, &t.Version, &t.LastOp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.OccurredAt, t.CreatedAt, t.UpdatedAt = ParseTS(occ), ParseTS(c), ParseTS(u)
	if t.Merchant, err = s.Open("tx.merchant", m); err != nil {
		return nil, err
	}
	if t.Description, err = s.Open("tx.description", d); err != nil {
		return nil, err
	}
	if t.Notes, err = s.Open("tx.notes", n); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) CreateTransaction(ctx context.Context, userID string, sl *crypto.Sealer, t *Transaction) error {
	now := s.Clock.Now()
	t.CreatedAt, t.UpdatedAt, t.Version = now, now, 1
	var hash any
	if len(t.MessageHash) > 0 {
		hash = t.MessageHash
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO transactions (id, user_id, account_id, category_id, amount_cents, currency, occurred_at, kind,
		merchant_enc, description_enc, notes_enc, source, status, message_hash, parser, confidence, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, userID, t.AccountID, t.CategoryID, t.AmountCents, t.Currency, TS(t.OccurredAt), t.Kind,
		sl.Seal("tx.merchant", t.Merchant), sl.Seal("tx.description", t.Description), sl.Seal("tx.notes", t.Notes),
		t.Source, t.Status, hash, t.Parser, t.Confidence, TS(now), TS(now))
	if isUnique(err) {
		return ErrDuplicate
	}
	if isFK(err) {
		return ErrNotFound // cuenta o categoría inexistente o de otro usuario
	}
	return err
}

func (s *Store) Transaction(ctx context.Context, userID string, sl *crypto.Sealer, id string) (*Transaction, error) {
	return scanTx(s.DB.QueryRowContext(ctx, `SELECT `+txCols+` FROM transactions WHERE user_id = ? AND id = ?`, userID, id), sl)
}

func (s *Store) UpdateTransaction(ctx context.Context, userID string, sl *crypto.Sealer, t *Transaction) error {
	return s.UpdateTransactionIf(ctx, userID, sl, t, 0, "")
}

// VersionConflict: el registro cambió (otro dispositivo) desde la versión que el cliente conocía.
type VersionConflict struct{ Current int64 }

func (e *VersionConflict) Error() string { return "el registro cambió en otro dispositivo" }
func (e *VersionConflict) Unwrap() error { return ErrConflict }

// UpdateTransactionIf guarda solo si la versión sigue siendo expect (0 = sin condición) y anota op,
// el identificador de la operación del cliente. La comprobación va en el mismo UPDATE: sin carreras.
func (s *Store) UpdateTransactionIf(ctx context.Context, userID string, sl *crypto.Sealer, t *Transaction, expect int64, op string) error {
	t.UpdatedAt = s.Clock.Now()
	res, err := s.DB.ExecContext(ctx, `UPDATE transactions SET account_id = ?, category_id = ?, amount_cents = ?, currency = ?, occurred_at = ?, kind = ?,
		merchant_enc = ?, description_enc = ?, notes_enc = ?, status = ?, updated_at = ?, version = version + 1, last_op = ?
		WHERE id = ? AND user_id = ? AND (? = 0 OR version = ?)`,
		t.AccountID, t.CategoryID, t.AmountCents, t.Currency, TS(t.OccurredAt), t.Kind,
		sl.Seal("tx.merchant", t.Merchant), sl.Seal("tx.description", t.Description), sl.Seal("tx.notes", t.Notes),
		t.Status, TS(t.UpdatedAt), op, t.ID, userID, expect, expect)
	if isFK(err) {
		return ErrNotFound
	}
	if err := s.versionResult(ctx, res, err, "transactions", userID, t.ID); err != nil {
		return err
	}
	t.LastOp = op
	return s.DB.QueryRowContext(ctx, `SELECT version FROM transactions WHERE id = ? AND user_id = ?`, t.ID, userID).Scan(&t.Version)
}

// versionResult distingue "no existe" de "existe con otra versión" cuando un UPDATE/DELETE condicionado no tocó filas.
func (s *Store) versionResult(ctx context.Context, res sql.Result, err error, table, userID, id string) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var v int64
	// table es siempre una constante del paquete, nunca un dato del usuario.
	err = s.DB.QueryRowContext(ctx, `SELECT version FROM `+table+` WHERE id = ? AND user_id = ?`, id, userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return &VersionConflict{Current: v}
}

func (s *Store) DeleteTransaction(ctx context.Context, userID, id string) error {
	return s.DeleteTransactionIf(ctx, userID, id, 0)
}

// DeleteTransactionIf borra solo si la versión sigue siendo expect (0 = sin condición).
func (s *Store) DeleteTransactionIf(ctx context.Context, userID, id string, expect int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM transactions WHERE id = ? AND user_id = ? AND (? = 0 OR version = ?)`, id, userID, expect, expect)
	return s.versionResult(ctx, res, err, "transactions", userID, id)
}

type TxFilter struct {
	From, To    time.Time // [From, To) en UTC; cero = sin límite
	AccountID   string
	CategoryIDs []string
	Status      string
	Source      string
	Kind        string
	Limit       int // 0 = sin límite
}

// Transactions lista movimientos del usuario, del más reciente al más antiguo.
func (s *Store) Transactions(ctx context.Context, userID string, sl *crypto.Sealer, f TxFilter) ([]Transaction, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT ` + txCols + ` FROM transactions WHERE user_id = ?`)
	args := []any{userID}
	if !f.From.IsZero() {
		q.WriteString(` AND occurred_at >= ?`)
		args = append(args, TS(f.From))
	}
	if !f.To.IsZero() {
		q.WriteString(` AND occurred_at < ?`)
		args = append(args, TS(f.To))
	}
	for col, v := range map[string]string{"account_id": f.AccountID, "status": f.Status, "source": f.Source, "kind": f.Kind} {
		if v != "" {
			q.WriteString(` AND ` + col + ` = ?`)
			args = append(args, v)
		}
	}
	if len(f.CategoryIDs) > 0 {
		q.WriteString(` AND category_id IN (?` + strings.Repeat(",?", len(f.CategoryIDs)-1) + `)`)
		for _, c := range f.CategoryIDs {
			args = append(args, c)
		}
	}
	q.WriteString(` ORDER BY occurred_at DESC, id DESC`)
	if f.Limit > 0 {
		q.WriteString(` LIMIT ?`)
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Transaction{}
	for rows.Next() {
		t, err := scanTx(rows, sl)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// SpentByCategory suma compras menos reversos por categoría en [from, to).
func (s *Store) SpentByCategory(ctx context.Context, userID string, from, to time.Time) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT category_id, SUM(CASE kind WHEN 'refund' THEN -amount_cents ELSE amount_cents END)
		FROM transactions WHERE user_id = ? AND occurred_at >= ? AND occurred_at < ? GROUP BY category_id`, userID, TS(from), TS(to))
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

func (s *Store) CountPending(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions WHERE user_id = ? AND status = 'pending_review'`, userID).Scan(&n)
	return n, err
}

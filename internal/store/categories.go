package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Category struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parent_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	Icon      string    `json:"icon"`
	IsSystem  bool      `json:"is_system"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const catCols = `id, COALESCE(parent_id, ''), name, color, icon, is_system, archived, created_at, updated_at`

func scanCategory(row interface{ Scan(...any) error }) (*Category, error) {
	var c Category
	var sys, arch int
	var cr, up string
	err := row.Scan(&c.ID, &c.ParentID, &c.Name, &c.Color, &c.Icon, &sys, &arch, &cr, &up)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	c.IsSystem, c.Archived, c.CreatedAt, c.UpdatedAt = sys == 1, arch == 1, ParseTS(cr), ParseTS(up)
	return &c, err
}

func (s *Store) Categories(ctx context.Context, userID string) ([]Category, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+catCols+` FROM categories WHERE user_id = ? ORDER BY is_system, name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) Category(ctx context.Context, userID, id string) (*Category, error) {
	return scanCategory(s.DB.QueryRowContext(ctx, `SELECT `+catCols+` FROM categories WHERE user_id = ? AND id = ?`, userID, id))
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func insertCategory(ctx context.Context, ex execer, userID string, c *Category) error {
	ts := TS(c.CreatedAt)
	_, err := ex.ExecContext(ctx, `INSERT INTO categories (id, user_id, parent_id, name, color, icon, is_system, archived, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID, userID, nullStr(c.ParentID), c.Name, c.Color, c.Icon, boolInt(c.IsSystem), boolInt(c.Archived), ts, ts)
	if isUnique(err) {
		return Conflict("ya existe una categoría con ese nombre")
	}
	if isFK(err) {
		return ErrNotFound
	}
	return err
}

func (s *Store) CreateCategory(ctx context.Context, userID string, c *Category) error {
	c.CreatedAt = s.Clock.Now()
	c.UpdatedAt = c.CreatedAt
	return insertCategory(ctx, s.DB, userID, c)
}

func (s *Store) CreateCategoryTx(ctx context.Context, tx *sql.Tx, userID string, c *Category) error {
	c.CreatedAt = s.Clock.Now()
	c.UpdatedAt = c.CreatedAt
	return insertCategory(ctx, tx, userID, c)
}

func (s *Store) UpdateCategory(ctx context.Context, userID string, c *Category) error {
	err := affected(s.DB.ExecContext(ctx, `UPDATE categories SET parent_id = ?, name = ?, color = ?, icon = ?, archived = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, nullStr(c.ParentID), c.Name, c.Color, c.Icon, boolInt(c.Archived), s.now(), c.ID, userID))
	if isUnique(err) {
		return Conflict("ya existe una categoría con ese nombre")
	}
	return err
}

func (s *Store) HasChildren(ctx context.Context, userID, id string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories WHERE user_id = ? AND parent_id = ?`, userID, id).Scan(&n)
	return n > 0, err
}

// SystemCategory devuelve "Otros", donde caen los gastos de categorías borradas.
func (s *Store) SystemCategory(ctx context.Context, userID string) (*Category, error) {
	return scanCategory(s.DB.QueryRowContext(ctx, `SELECT `+catCols+` FROM categories WHERE user_id = ? AND is_system = 1 LIMIT 1`, userID))
}

// DeleteCategory reasigna sus gastos a "Otros" y borra la categoría (y su presupuesto).
// Devuelve cuántos gastos se movieron.
func (s *Store) DeleteCategory(ctx context.Context, userID, id string) (int64, error) {
	c, err := s.Category(ctx, userID, id)
	if err != nil {
		return 0, err
	}
	if c.IsSystem {
		return 0, Conflict("la categoría del sistema no se puede borrar")
	}
	if has, err := s.HasChildren(ctx, userID, id); err != nil {
		return 0, err
	} else if has {
		return 0, Conflict("la categoría tiene subcategorías: muévelas o bórralas primero")
	}
	sys, err := s.SystemCategory(ctx, userID)
	if err != nil {
		return 0, err
	}
	var moved int64
	err = s.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE transactions SET category_id = ?, updated_at = ? WHERE user_id = ? AND category_id = ?`, sys.ID, s.now(), userID, id)
		if err != nil {
			return err
		}
		moved, _ = res.RowsAffected()
		return affected(tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ? AND user_id = ?`, id, userID))
	})
	return moved, err
}

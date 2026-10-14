// Package store concentra el acceso a SQLite. Regla de oro: toda función que toca
// datos de un usuario recibe userID y lo incluye en el WHERE; no existen consultas
// "por id" sin dueño.
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"pulse/internal/clock"
)

var (
	ErrNotFound = errors.New("no encontrado")
	ErrConflict = errors.New("conflicto")
)

// ConflictError lleva un mensaje para el usuario.
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }
func (e *ConflictError) Unwrap() error { return ErrConflict }

func Conflict(msg string) error { return &ConflictError{msg} }

const tsLayout = "2006-01-02T15:04:05Z"

// TS formatea en UTC con ancho fijo (ordenable como texto).
func TS(t time.Time) string { return t.UTC().Format(tsLayout) }

func ParseTS(s string) time.Time {
	t, _ := time.Parse(tsLayout, s)
	return t
}

func parseNullTS(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := ParseTS(s.String)
	return &t
}

type Store struct {
	DB    *sql.DB
	Clock clock.Clock
}

func (s *Store) now() string { return TS(s.Clock.Now()) }

// Tx ejecuta fn dentro de una transacción.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isFK(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

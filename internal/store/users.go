package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	DEKEnc       []byte
	Timezone     string
	Theme        string
	FailedLogins int
	LockedUntil  *time.Time
	DisabledAt   *time.Time
	CreatedAt    time.Time
}

const userCols = `id, email, password_hash, role, dek_enc, timezone, theme, failed_logins, locked_until, disabled_at, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var locked, disabled sql.NullString
	var created string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.DEKEnc, &u.Timezone, &u.Theme, &u.FailedLogins, &locked, &disabled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.LockedUntil, u.DisabledAt, u.CreatedAt = parseNullTS(locked), parseNullTS(disabled), ParseTS(created)
	return &u, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
}

func (s *Store) User(ctx context.Context, id string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func insertUser(ctx context.Context, tx execer, u *User) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO users (id, email, password_hash, role, dek_enc, timezone, theme, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'system', ?)`, u.ID, u.Email, u.PasswordHash, u.Role, u.DEKEnc, u.Timezone, TS(u.CreatedAt))
	if isUnique(err) {
		return Conflict("email ya registrado")
	}
	return err
}

// RegisterFailure suma un fallo y aplica el bloqueo calculado por lockFn.
func (s *Store) RegisterFailure(ctx context.Context, userID string, lockFn func(failures int) (time.Time, bool)) (int, error) {
	var n int
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `UPDATE users SET failed_logins = failed_logins + 1 WHERE id = ? RETURNING failed_logins`, userID).Scan(&n); err != nil {
			return err
		}
		if until, ok := lockFn(n); ok {
			_, err := tx.ExecContext(ctx, `UPDATE users SET locked_until = ? WHERE id = ?`, TS(until), userID)
			return err
		}
		return nil
	})
	return n, err
}

func (s *Store) ResetFailures(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET failed_logins = 0, locked_until = NULL WHERE id = ?`, userID)
	return err
}

func (s *Store) SetPassword(ctx context.Context, userID, hash string) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID))
}

func (s *Store) UpdatePrefs(ctx context.Context, userID string, theme, tz *string) error {
	if theme != nil {
		if err := affected(s.DB.ExecContext(ctx, `UPDATE users SET theme = ? WHERE id = ?`, *theme, userID)); err != nil {
			return err
		}
	}
	if tz != nil {
		return affected(s.DB.ExecContext(ctx, `UPDATE users SET timezone = ? WHERE id = ?`, *tz, userID))
	}
	return nil
}

func (s *Store) SetDisabled(ctx context.Context, email string, disabled bool) (*User, error) {
	u, err := s.UserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	var v any
	if disabled {
		v = s.now()
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, v, u.ID); err != nil {
		return nil, err
	}
	if disabled {
		_, err = s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, u.ID)
	}
	return u, err
}

// ResetMFA borra todos los factores y sesiones: el próximo login obliga a registrar uno nuevo.
func (s *Store) ResetMFA(ctx context.Context, userID string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM totp WHERE user_id = ?`, `DELETE FROM webauthn_credentials WHERE user_id = ?`,
			`DELETE FROM recovery_codes WHERE user_id = ?`, `DELETE FROM sessions WHERE user_id = ?`} {
			if _, err := tx.ExecContext(ctx, q, userID); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteUser borra la cuenta y, en cascada, todos sus datos (cuentas, movimientos, presupuestos,
// reglas, sesiones, MFA, push, Gmail). El registro de seguridad no tiene clave foránea: se conserva
// hasta que vence su retención (AuditRetention), como declara /privacidad.
func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID))
}

package store

import (
	"context"
	"database/sql"
	"time"
)

type Invite struct {
	ID        string
	Email     string
	Role      string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	UsedBy    string
	RevokedAt *time.Time
}

func (s *Store) CreateInvite(ctx context.Context, id string, codeHash []byte, email, role string, expires time.Time) error {
	var e any
	if email != "" {
		e = email
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO invites (id, code_hash, email, role, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, codeHash, e, role, s.now(), TS(expires))
	return err
}

func (s *Store) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, COALESCE(email,''), role, created_at, expires_at, used_at, COALESCE(used_by,''), revoked_at FROM invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		var i Invite
		var c, e string
		var used, rev sql.NullString
		if err := rows.Scan(&i.ID, &i.Email, &i.Role, &c, &e, &used, &i.UsedBy, &rev); err != nil {
			return nil, err
		}
		i.CreatedAt, i.ExpiresAt, i.UsedAt, i.RevokedAt = ParseTS(c), ParseTS(e), parseNullTS(used), parseNullTS(rev)
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) RevokeInvite(ctx context.Context, id string) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE invites SET revoked_at = ? WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL`, s.now(), id))
}

// NewUserInput agrupa lo necesario para registrar.
type NewUserInput struct {
	User     *User
	CodeHash []byte
	Setup    func(ctx context.Context, tx *sql.Tx) error // datos iniciales (categorías, cuenta)
}

var ErrInvite = Conflict("invitación inválida")

// RegisterWithInvite consume la invitación y crea al usuario de forma atómica.
// El UPDATE condicional impide que dos registros simultáneos usen el mismo código.
func (s *Store) RegisterWithInvite(ctx context.Context, in NewUserInput) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		var id, role string
		var email sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id, email, role FROM invites
			WHERE code_hash = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`, in.CodeHash, s.now()).Scan(&id, &email, &role)
		if err == sql.ErrNoRows {
			return ErrInvite
		}
		if err != nil {
			return err
		}
		if email.Valid && email.String != "" && !equalFold(email.String, in.User.Email) {
			return ErrInvite
		}
		in.User.Role = role
		if err := insertUser(ctx, tx, in.User); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE invites SET used_at = ?, used_by = ? WHERE id = ? AND used_at IS NULL`, s.now(), in.User.ID, id)
		if err := affected(res, err); err != nil {
			return ErrInvite
		}
		if in.Setup != nil {
			return in.Setup(ctx, tx)
		}
		return nil
	})
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 32
		}
		if 'A' <= y && y <= 'Z' {
			y += 32
		}
		if x != y {
			return false
		}
	}
	return true
}

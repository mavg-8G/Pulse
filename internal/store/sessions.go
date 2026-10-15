package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Session struct {
	ID                string
	UserID            string
	State             string // enroll | mfa | full
	Remember          bool
	IP, UserAgent     string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	StepUpAt          *time.Time
}

func (s *Store) CreateSession(ctx context.Context, se *Session, tokenHash []byte) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions (id, token_hash, user_id, state, remember, ip, user_agent, created_at, last_seen_at, expires_at, absolute_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, se.ID, tokenHash, se.UserID, se.State, boolInt(se.Remember), se.IP, se.UserAgent,
		TS(se.CreatedAt), TS(se.LastSeenAt), TS(se.ExpiresAt), TS(se.AbsoluteExpiresAt))
	return err
}

const sessCols = `id, user_id, state, remember, ip, user_agent, created_at, last_seen_at, expires_at, absolute_expires_at, stepup_at`

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	var se Session
	var rem int
	var c, l, e, a string
	var st sql.NullString
	err := row.Scan(&se.ID, &se.UserID, &se.State, &rem, &se.IP, &se.UserAgent, &c, &l, &e, &a, &st)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	se.Remember = rem == 1
	se.CreatedAt, se.LastSeenAt, se.ExpiresAt, se.AbsoluteExpiresAt, se.StepUpAt = ParseTS(c), ParseTS(l), ParseTS(e), ParseTS(a), parseNullTS(st)
	return &se, nil
}

func (s *Store) SessionByToken(ctx context.Context, tokenHash []byte) (*Session, error) {
	return scanSession(s.DB.QueryRowContext(ctx, `SELECT `+sessCols+` FROM sessions WHERE token_hash = ?`, tokenHash))
}

func (s *Store) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sessCols+` FROM sessions WHERE user_id = ? AND state = 'full' ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		se, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *se)
	}
	return out, rows.Err()
}

func (s *Store) TouchSession(ctx context.Context, id string, lastSeen, expires time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, TS(lastSeen), TS(expires), id)
	return err
}

func (s *Store) SetStepUp(ctx context.Context, id string, t time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET stepup_at = ? WHERE id = ?`, TS(t), id)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, userID, id string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ?`, id, userID))
}

func (s *Store) DeleteOtherSessions(ctx context.Context, userID, keepID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keepID)
	return err
}

// AuditRetention es cuánto se guarda el registro de seguridad (IP, navegador, evento).
// Debe coincidir con web/public/privacidad.html.
const AuditRetention = 365 * 24 * time.Hour

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	now := s.now()
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ? OR absolute_expires_at <= ?`, now, now)
	if err == nil {
		_, err = s.DB.ExecContext(ctx, `DELETE FROM auth_challenges WHERE expires_at <= ?`, now)
	}
	if err == nil { // retención declarada en /privacidad
		_, err = s.DB.ExecContext(ctx, `DELETE FROM audit_log WHERE created_at < ?`, TS(s.Clock.Now().Add(-AuditRetention)))
	}
	return err
}

// Challenges WebAuthn/TOTP pendientes, atados a la sesión.
func (s *Store) PutChallenge(ctx context.Context, sessionID, purpose string, data []byte, expires time.Time) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO auth_challenges (session_id, purpose, data, expires_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (session_id, purpose) DO UPDATE SET data = excluded.data, expires_at = excluded.expires_at`, sessionID, purpose, data, TS(expires))
	return err
}

// TakeChallenge lee y borra el desafío (un solo uso).
func (s *Store) TakeChallenge(ctx context.Context, sessionID, purpose string) ([]byte, error) {
	var data []byte
	err := s.DB.QueryRowContext(ctx, `DELETE FROM auth_challenges WHERE session_id = ? AND purpose = ? AND expires_at > ? RETURNING data`,
		sessionID, purpose, s.now()).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}

// GetChallenge lee sin consumir (para TOTP, donde el usuario puede reintentar el código).
func (s *Store) GetChallenge(ctx context.Context, sessionID, purpose string) ([]byte, error) {
	var data []byte
	err := s.DB.QueryRowContext(ctx, `SELECT data FROM auth_challenges WHERE session_id = ? AND purpose = ? AND expires_at > ?`,
		sessionID, purpose, s.now()).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}

func (s *Store) DeleteChallenge(ctx context.Context, sessionID, purpose string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM auth_challenges WHERE session_id = ? AND purpose = ?`, sessionID, purpose)
	return err
}

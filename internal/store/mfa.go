package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type TOTP struct {
	SecretEnc   []byte
	ConfirmedAt *time.Time
	LastStep    int64
}

func (s *Store) TOTP(ctx context.Context, userID string) (*TOTP, error) {
	var t TOTP
	var c sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT secret_enc, confirmed_at, last_step FROM totp WHERE user_id = ?`, userID).Scan(&t.SecretEnc, &c, &t.LastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.ConfirmedAt = parseNullTS(c)
	return &t, err
}

// ConfirmTOTP guarda (o reemplaza) el secreto ya verificado. El secreto pendiente de
// confirmar vive en auth_challenges, así el anterior sigue valiendo hasta confirmar el nuevo.
func (s *Store) ConfirmTOTP(ctx context.Context, userID string, secretEnc []byte, step int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO totp (user_id, secret_enc, confirmed_at, last_step) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET secret_enc = excluded.secret_enc, confirmed_at = excluded.confirmed_at, last_step = excluded.last_step`,
		userID, secretEnc, s.now(), step)
	return err
}

// UseTOTPStep registra el paso usado solo si es mayor al último (anti-replay atómico).
func (s *Store) UseTOTPStep(ctx context.Context, userID string, step int64) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE totp SET last_step = ? WHERE user_id = ? AND last_step < ?`, step, userID, step))
}

func (s *Store) DeleteTOTP(ctx context.Context, userID string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM totp WHERE user_id = ?`, userID))
}

type Passkey struct {
	ID           string
	CredentialID []byte
	PublicKey    []byte
	Alg          int64
	SignCount    uint32
	Name         string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

func (s *Store) AddPasskey(ctx context.Context, userID string, p *Passkey) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO webauthn_credentials (id, user_id, credential_id, public_key, alg, sign_count, name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.ID, userID, p.CredentialID, p.PublicKey, p.Alg, p.SignCount, p.Name, s.now())
	if isUnique(err) {
		return Conflict("esta passkey ya está registrada")
	}
	return err
}

func (s *Store) Passkeys(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, credential_id, public_key, alg, sign_count, name, created_at, last_used_at
		FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		var c string
		var l sql.NullString
		if err := rows.Scan(&p.ID, &p.CredentialID, &p.PublicKey, &p.Alg, &p.SignCount, &p.Name, &c, &l); err != nil {
			return nil, err
		}
		p.CreatedAt, p.LastUsedAt = ParseTS(c), parseNullTS(l)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePasskeyCounter(ctx context.Context, userID string, credID []byte, count uint32) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE webauthn_credentials SET sign_count = ?, last_used_at = ? WHERE user_id = ? AND credential_id = ?`,
		count, s.now(), userID, credID))
}

func (s *Store) DeletePasskey(ctx context.Context, userID, id string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?`, id, userID))
}

// MFAStatus resume los factores del usuario.
type MFAStatus struct {
	TOTP              bool `json:"totp"`
	Passkeys          int  `json:"passkeys"`
	RecoveryRemaining int  `json:"recovery_remaining"`
}

func (m MFAStatus) Any() bool { return m.TOTP || m.Passkeys > 0 }

func (s *Store) MFA(ctx context.Context, userID string) (MFAStatus, error) {
	var m MFAStatus
	var t int
	err := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM totp WHERE user_id = ?1 AND confirmed_at IS NOT NULL),
		(SELECT COUNT(*) FROM webauthn_credentials WHERE user_id = ?1),
		(SELECT COUNT(*) FROM recovery_codes WHERE user_id = ?1 AND used_at IS NULL)`, userID).Scan(&t, &m.Passkeys, &m.RecoveryRemaining)
	m.TOTP = t > 0
	return m, err
}

func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID string, hashes [][]byte) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
			return err
		}
		for _, h := range hashes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, h); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) UseRecoveryCode(ctx context.Context, userID string, hash []byte) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, s.now(), userID, hash))
}

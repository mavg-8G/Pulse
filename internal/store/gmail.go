package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GmailConnection es la conexión de un usuario con su Gmail. EmailEnc y RefreshEnc van
// cifrados con la DEK del usuario; aquí nunca hay nada en claro.
type GmailConnection struct {
	UserID         string
	EmailEnc       []byte
	RefreshEnc     []byte
	Status         string // active | broken
	LastError      string // código corto, sin datos del correo
	HistoryID      uint64
	LastSyncAt     *time.Time
	LastOKAt       *time.Time
	NextSyncAt     time.Time
	Failures       int
	WatchExpiresAt *time.Time
	Shadow         bool
	Gemini         bool
	Imported       int
	CreatedAt      time.Time
}

const gmailCols = `user_id, email_enc, refresh_enc, status, last_error, history_id, last_sync_at, last_ok_at, next_sync_at,
	failures, watch_expires_at, shadow, gemini, imported, created_at`

func scanGmail(row interface{ Scan(...any) error }) (*GmailConnection, error) {
	var g GmailConnection
	var lastSync, lastOK, watch sql.NullString
	var next, created string
	var shadow, gemini int
	err := row.Scan(&g.UserID, &g.EmailEnc, &g.RefreshEnc, &g.Status, &g.LastError, &g.HistoryID, &lastSync, &lastOK, &next,
		&g.Failures, &watch, &shadow, &gemini, &g.Imported, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.LastSyncAt, g.LastOKAt, g.WatchExpiresAt = parseNullTS(lastSync), parseNullTS(lastOK), parseNullTS(watch)
	g.NextSyncAt, g.CreatedAt = ParseTS(next), ParseTS(created)
	g.Shadow, g.Gemini = shadow == 1, gemini == 1
	return &g, nil
}

// SaveGmail crea o reemplaza la conexión (reconectar renueva el token y la reactiva).
// Una misma cuenta de Gmail no puede estar conectada a dos usuarios de Pulse.
func (s *Store) SaveGmail(ctx context.Context, userID string, emailEnc, emailHash, refreshEnc []byte) error {
	now := s.now()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO gmail_connections (user_id, email_enc, email_hash, refresh_enc, status, next_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET email_enc = excluded.email_enc, email_hash = excluded.email_hash, refresh_enc = excluded.refresh_enc,
			status = 'active', last_error = '', failures = 0, next_sync_at = excluded.next_sync_at, history_id = 0, watch_expires_at = NULL,
			updated_at = excluded.updated_at`,
		userID, emailEnc, emailHash, refreshEnc, now, now, now)
	if isUnique(err) {
		return Conflict("Esa cuenta de Gmail ya está conectada a otro usuario de Pulse.")
	}
	return err
}

func (s *Store) Gmail(ctx context.Context, userID string) (*GmailConnection, error) {
	return scanGmail(s.DB.QueryRowContext(ctx, `SELECT `+gmailCols+` FROM gmail_connections WHERE user_id = ?`, userID))
}

// GmailUserByEmailHash resuelve el aviso de Pub/Sub (que trae el email) a un usuario.
func (s *Store) GmailUserByEmailHash(ctx context.Context, h []byte) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT user_id FROM gmail_connections WHERE email_hash = ? AND status = 'active'`, h).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// DueGmail devuelve los usuarios cuya sincronización toca (activos y con la hora cumplida).
func (s *Store) DueGmail(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT g.user_id FROM gmail_connections g JOIN users u ON u.id = g.user_id
		WHERE g.status = 'active' AND u.disabled_at IS NULL AND g.next_sync_at <= ? ORDER BY g.next_sync_at`, s.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GmailSynced registra una sincronización correcta.
func (s *Store) GmailSynced(ctx context.Context, userID string, next time.Time, imported int) error {
	now := s.now()
	return affected(s.DB.ExecContext(ctx, `UPDATE gmail_connections SET last_sync_at = ?, last_ok_at = ?, next_sync_at = ?, failures = 0,
		last_error = '', imported = imported + ?, updated_at = ? WHERE user_id = ?`, now, now, TS(next), imported, now, userID))
}

// GmailFailed registra un fallo temporal: se reintenta en next.
func (s *Store) GmailFailed(ctx context.Context, userID, code string, next time.Time) error {
	now := s.now()
	return affected(s.DB.ExecContext(ctx, `UPDATE gmail_connections SET last_sync_at = ?, next_sync_at = ?, failures = failures + 1,
		last_error = ?, updated_at = ? WHERE user_id = ?`, now, TS(next), code, now, userID))
}

// GmailBroken marca la conexión como rota (token revocado o sin permiso): no se reintenta
// hasta que la persona vuelva a conectar.
func (s *Store) GmailBroken(ctx context.Context, userID, code string) error {
	now := s.now()
	return affected(s.DB.ExecContext(ctx, `UPDATE gmail_connections SET status = 'broken', last_sync_at = ?, last_error = ?, watch_expires_at = NULL,
		updated_at = ? WHERE user_id = ?`, now, code, now, userID))
}

// GmailTrigger adelanta la próxima sincronización (aviso de Pub/Sub o botón "Sincronizar").
// Solo avanza history_id: un aviso viejo o repetido no hace nada nuevo.
func (s *Store) GmailTrigger(ctx context.Context, userID string, historyID uint64) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE gmail_connections SET next_sync_at = ?, history_id = MAX(history_id, ?), updated_at = ?
		WHERE user_id = ? AND status = 'active' AND (? = 0 OR ? > history_id)`, s.now(), historyID, s.now(), userID, historyID, historyID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) GmailWatched(ctx context.Context, userID string, historyID uint64, expires time.Time) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE gmail_connections SET watch_expires_at = ?, history_id = MAX(history_id, ?), updated_at = ?
		WHERE user_id = ?`, TS(expires), historyID, s.now(), userID))
}

func (s *Store) SetGmailOptions(ctx context.Context, userID string, shadow, gemini *bool) error {
	if shadow != nil {
		if err := affected(s.DB.ExecContext(ctx, `UPDATE gmail_connections SET shadow = ?, updated_at = ? WHERE user_id = ?`, boolInt(*shadow), s.now(), userID)); err != nil {
			return err
		}
	}
	if gemini != nil {
		return affected(s.DB.ExecContext(ctx, `UPDATE gmail_connections SET gemini = ?, updated_at = ? WHERE user_id = ?`, boolInt(*gemini), s.now(), userID))
	}
	return nil
}

// DeleteGmail borra la conexión y la lista de mensajes vistos (los gastos importados se quedan).
func (s *Store) DeleteGmail(ctx context.Context, userID string) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM gmail_seen WHERE user_id = ?`, userID); err != nil {
			return err
		}
		return affected(tx.ExecContext(ctx, `DELETE FROM gmail_connections WHERE user_id = ?`, userID))
	})
}

// ClaimGmailMessage reserva un mensaje para procesarlo. Devuelve false si ya estaba:
// así el mismo correo que llega por Pub/Sub y por polling a la vez se procesa una sola vez.
func (s *Store) ClaimGmailMessage(ctx context.Context, userID string, h []byte) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO gmail_seen (user_id, msg_hash, seen_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, userID, h, s.now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ReleaseGmailMessage deshace la reserva si el procesamiento falló por algo temporal.
func (s *Store) ReleaseGmailMessage(ctx context.Context, userID string, h []byte) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM gmail_seen WHERE user_id = ? AND msg_hash = ?`, userID, h)
	return err
}

// PruneGmailSeen olvida los mensajes vistos hace más de 60 días (la consulta solo mira días recientes).
func (s *Store) PruneGmailSeen(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM gmail_seen WHERE seen_at < ?`, TS(s.Clock.Now().Add(-60*24*time.Hour)))
	return err
}

// ConfirmImported confirma un gasto importado (modo sombra apagado y alta confianza).
func (s *Store) ConfirmImported(ctx context.Context, userID, txID string) error {
	return affected(s.DB.ExecContext(ctx, `UPDATE transactions SET status = 'confirmed', updated_at = ? WHERE id = ? AND user_id = ? AND status = 'pending_review'`,
		s.now(), txID, userID))
}

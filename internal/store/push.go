package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type NotificationPreferences struct {
	Budgets      bool `json:"budgets"`
	Security     bool `json:"security"`
	Weekly       bool `json:"weekly"`
	Monthly      bool `json:"monthly"`
	HideAmounts  bool `json:"hide_amounts"`
	QuietEnabled bool `json:"quiet_enabled"`
	QuietStart   int  `json:"quiet_start"`
	QuietEnd     int  `json:"quiet_end"`
}

func DefaultNotificationPreferences() NotificationPreferences {
	return NotificationPreferences{Budgets: true, Security: true, Weekly: true, Monthly: true, HideAmounts: true, QuietStart: 1320, QuietEnd: 480}
}

func (s *Store) NotificationPrefs(ctx context.Context, uid string) (NotificationPreferences, error) {
	p := DefaultNotificationPreferences()
	err := s.DB.QueryRowContext(ctx, `SELECT budgets,security,weekly,monthly,hide_amounts,quiet_enabled,quiet_start,quiet_end FROM notification_preferences WHERE user_id=?`, uid).Scan(&p.Budgets, &p.Security, &p.Weekly, &p.Monthly, &p.HideAmounts, &p.QuietEnabled, &p.QuietStart, &p.QuietEnd)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}

func (s *Store) SetNotificationPrefs(ctx context.Context, uid string, p NotificationPreferences) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO notification_preferences(user_id,budgets,security,weekly,monthly,hide_amounts,quiet_enabled,quiet_start,quiet_end) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET budgets=excluded.budgets,security=excluded.security,weekly=excluded.weekly,monthly=excluded.monthly,hide_amounts=excluded.hide_amounts,quiet_enabled=excluded.quiet_enabled,quiet_start=excluded.quiet_start,quiet_end=excluded.quiet_end`, uid, p.Budgets, p.Security, p.Weekly, p.Monthly, p.HideAmounts, p.QuietEnabled, p.QuietStart, p.QuietEnd)
	return err
}

type PushSubscription struct {
	ID             string    `json:"id"`
	UserID         string    `json:"-"`
	SessionID      string    `json:"-"`
	EndpointHash   []byte    `json:"-"`
	Enc            []byte    `json:"-"`
	DeviceLabel    string    `json:"device_label"`
	SecurityCursor int64     `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Store) SavePushSubscription(ctx context.Context, p *PushSubscription) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		var owner, existing string
		err := tx.QueryRowContext(ctx, `SELECT user_id,id FROM push_subscriptions WHERE endpoint_hash=?`, p.EndpointHash).Scan(&owner, &existing)
		if err == nil {
			if owner != p.UserID {
				return Conflict("Este navegador tiene notificaciones de otra cuenta. Desactívalas antes de cambiar de cuenta.")
			}
			p.ID = existing
			_, err = tx.ExecContext(ctx, `UPDATE push_subscriptions SET subscription_enc=?,device_label=?,session_id=? WHERE id=? AND user_id=?`, p.Enc, p.DeviceLabel, p.SessionID, p.ID, p.UserID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id=?`, p.UserID).Scan(&count); err != nil {
			return err
		}
		if count >= 20 {
			return Conflict("Puedes activar hasta 20 dispositivos. Quita uno antes de agregar otro.")
		}
		p.CreatedAt = s.Clock.Now()
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM audit_log WHERE user_id=?`, p.UserID).Scan(&p.SecurityCursor); err != nil {
			return err
		}
		return affected(tx.ExecContext(ctx, `INSERT INTO push_subscriptions(id,user_id,session_id,endpoint_hash,subscription_enc,device_label,created_at,security_cursor) SELECT ?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM sessions WHERE id=? AND user_id=? AND state='full')`, p.ID, p.UserID, p.SessionID, p.EndpointHash, p.Enc, p.DeviceLabel, TS(p.CreatedAt), p.SecurityCursor, p.SessionID, p.UserID))
	})
}

func (s *Store) PushSubscriptions(ctx context.Context, uid string) ([]PushSubscription, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id,p.session_id,p.device_label,p.created_at,p.subscription_enc,p.security_cursor FROM push_subscriptions p JOIN sessions se ON se.id=p.session_id WHERE p.user_id=? AND se.user_id=p.user_id AND se.expires_at>? AND se.absolute_expires_at>?`, uid, s.now(), s.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushSubscription{}
	for rows.Next() {
		p := PushSubscription{UserID: uid}
		var ts string
		if err := rows.Scan(&p.ID, &p.SessionID, &p.DeviceLabel, &ts, &p.Enc, &p.SecurityCursor); err != nil {
			return nil, err
		}
		p.CreatedAt = ParseTS(ts)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeletePushSubscription(ctx context.Context, uid, id string) error {
	return affected(s.DB.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id=? AND id=?`, uid, id))
}

type PushDelivery struct {
	ID, UserID, SubscriptionID, Kind string
	Enc                              []byte
	Attempts                         int
}

func (s *Store) QueuePush(ctx context.Context, p PushSubscription, id, key, kind string, enc []byte) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO push_deliveries(id,user_id,subscription_id,event_key,kind,payload_enc,next_attempt_at,expires_at) SELECT ?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM push_subscriptions WHERE id=? AND user_id=?)`, id, p.UserID, p.ID, key, kind, enc, s.now(), TS(s.Clock.Now().Add(48*time.Hour)), p.ID, p.UserID)
	return err
}

func (s *Store) DuePush(ctx context.Context, uid string) ([]PushDelivery, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,subscription_id,kind,payload_enc,attempts FROM push_deliveries WHERE user_id=? AND completed_at IS NULL AND next_attempt_at<=? AND expires_at>? ORDER BY next_attempt_at,id LIMIT 100`, uid, s.now(), s.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushDelivery{}
	for rows.Next() {
		d := PushDelivery{UserID: uid}
		if err := rows.Scan(&d.ID, &d.SubscriptionID, &d.Kind, &d.Enc, &d.Attempts); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) CompletePush(ctx context.Context, uid, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE push_deliveries SET completed_at=? WHERE user_id=? AND id=?`, s.now(), uid, id)
	return err
}
func (s *Store) RetryPush(ctx context.Context, uid, id string, next time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE push_deliveries SET attempts=attempts+1,next_attempt_at=? WHERE user_id=? AND id=?`, TS(next), uid, id)
	return err
}

func (s *Store) PrunePush(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM push_deliveries WHERE expires_at<?`, TS(s.Clock.Now().Add(-14*24*time.Hour)))
	return err
}

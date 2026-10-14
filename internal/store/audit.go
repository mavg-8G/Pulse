package store

import (
	"context"
	"encoding/json"
	"time"
)

type AuditEntry struct {
	ID        int64             `json:"id"`
	Event     string            `json:"event"`
	IP        string            `json:"ip"`
	UserAgent string            `json:"user_agent"`
	Meta      map[string]string `json:"meta"`
	CreatedAt time.Time         `json:"created_at"`
}

func (s *Store) Audit(ctx context.Context, userID, event, ip, ua string, meta map[string]string) {
	m, _ := json.Marshal(meta)
	if meta == nil {
		m = []byte("{}")
	}
	var uid any
	if userID != "" {
		uid = userID
	}
	// La auditoría nunca debe tumbar la petición; un fallo aquí se registra en el log del proceso.
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO audit_log (user_id, event, ip, user_agent, meta, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		uid, event, ip, ua, string(m), s.now())
}

func (s *Store) AuditFor(ctx context.Context, userID string, limit int) ([]AuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, event, ip, user_agent, meta, created_at FROM audit_log WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var m, c string
		if err := rows.Scan(&e.ID, &e.Event, &e.IP, &e.UserAgent, &m, &c); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(m), &e.Meta)
		e.CreatedAt = ParseTS(c)
		out = append(out, e)
	}
	return out, rows.Err()
}

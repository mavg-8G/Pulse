package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"
)

// AuditFailures permite monitorizar pérdidas de auditoría sin exponer datos personales.
var AuditFailures atomic.Uint64

type AuditEntry struct {
	ID        int64             `json:"id"`
	Event     string            `json:"event"`
	IP        string            `json:"ip"`
	UserAgent string            `json:"user_agent"`
	Meta      map[string]string `json:"meta"`
	CreatedAt time.Time         `json:"created_at"`
}

func (s *Store) Audit(ctx context.Context, userID, event, ip, ua string, meta map[string]string) error {
	m, _ := json.Marshal(meta)
	if meta == nil {
		m = []byte("{}")
	}
	var uid any
	if userID != "" {
		uid = userID
	}
	// El cierre del cliente no cancela la evidencia; el plazo limita la espera por SQLite.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO audit_log (user_id, event, ip, user_agent, meta, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		uid, event, ip, ua, string(m), s.now())
	if err != nil {
		AuditFailures.Add(1)
		// No registrar SQL, metadatos ni el error del driver: pueden contener secretos.
		log := s.Log
		if log == nil {
			log = slog.Default()
		}
		log.Error("audit_write_failed", "event", event)
	}
	return err
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

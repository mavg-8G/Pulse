package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BackupRun es un intento de respaldo externo. No guarda contenido ni credenciales.
type BackupRun struct {
	ID         string    `json:"id"`
	Trigger    string    `json:"trigger"` // manual | scheduled
	UserID     string    `json:"-"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Status     string    `json:"status"` // ok | failed
	Name       string    `json:"name"`
	SizeBytes  int64     `json:"size_bytes"`
	SHA256     string    `json:"sha256"`
	Attempts   int       `json:"attempts"`
	Error      string    `json:"error"`
}

func (s *Store) InsertBackupRun(ctx context.Context, r *BackupRun) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO backup_runs (id, trigger, user_id, started_at, finished_at, status, name, size_bytes, sha256, attempts, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.Trigger, nullable(r.UserID), TS(r.StartedAt), TS(r.FinishedAt), r.Status, r.Name,
		r.SizeBytes, r.SHA256, r.Attempts, r.Error)
	return err
}

const runCols = `id, trigger, COALESCE(user_id, ''), started_at, finished_at, status, name, size_bytes, sha256, attempts, error`

func scanRun(row interface{ Scan(...any) error }) (*BackupRun, error) {
	var r BackupRun
	var st, fin string
	err := row.Scan(&r.ID, &r.Trigger, &r.UserID, &st, &fin, &r.Status, &r.Name, &r.SizeBytes, &r.SHA256, &r.Attempts, &r.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	r.StartedAt, r.FinishedAt = ParseTS(st), ParseTS(fin)
	return &r, err
}

// BackupRuns devuelve los últimos intentos, del más reciente al más antiguo.
func (s *Store) BackupRuns(ctx context.Context, limit int) ([]BackupRun, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+runCols+` FROM backup_runs ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BackupRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// LastBackup devuelve el último intento con ese estado ("" = cualquiera) o ErrNotFound.
func (s *Store) LastBackup(ctx context.Context, status string) (*BackupRun, error) {
	return scanRun(s.DB.QueryRowContext(ctx, `SELECT `+runCols+` FROM backup_runs WHERE (? = '' OR status = ?)
		ORDER BY started_at DESC, id DESC LIMIT 1`, status, status))
}

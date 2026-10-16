// Package backup envía respaldos cifrados de la base fuera del servidor (fase 12).
//
//	VACUUM INTO (instantánea consistente) ──► age (claves públicas X25519) ──► S3 compatible o WebDAV
//
// Zero trust: el servidor solo tiene las claves PÚBLICAS de age, así que no puede leer sus propios respaldos;
// la copia en claro vive en una carpeta privada el tiempo justo para cifrarla y se borra pase lo que pase.
// El destino recibe solo bytes cifrados y autenticados, y la credencial del destino basta con que pueda escribir.
// Para leer un respaldo hacen falta dos secretos que no están en el servidor juntos con él: la identidad age
// (privada, fuera del VPS) y la KEK (los campos sensibles ya van cifrados por usuario dentro de la base).
package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"pulse/internal/auth"
	"pulse/internal/clock"
	"pulse/internal/db"
	"pulse/internal/store"
)

// LatestName es la última copia cifrada que queda en el servidor para descargarla desde Ajustes.
const LatestName = "ultimo-respaldo.db.age"

var ErrBusy = errors.New("ya hay un respaldo en curso")

type Service struct {
	DB         *sql.DB
	Store      *store.Store
	Dest       Destination
	Recipients []age.Recipient
	Dir        string        // carpeta privada de trabajo (p. ej. /data/backups)
	Interval   time.Duration // cada cuánto se respalda solo; 0 = solo a mano
	Attempts   int           // intentos de subida (por defecto 4)
	Backoff    time.Duration // espera antes del 2.º intento; se duplica (por defecto 5 s)
	Clock      clock.Clock
	Log        *slog.Logger
	// Alert avisa a los administradores (push). key evita repetir el aviso.
	Alert func(ctx context.Context, key, body string)
	// Sleep espera respetando ctx; los tests lo reemplazan para no dormir.
	Sleep func(ctx context.Context, d time.Duration) error

	mu      sync.Mutex
	running bool
}

// ParseRecipients lee una lista de claves públicas age separadas por comas o espacios.
func ParseRecipients(list string) ([]age.Recipient, error) {
	var out []age.Recipient
	for _, f := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		r, err := age.ParseX25519Recipient(f)
		if err != nil {
			return nil, fmt.Errorf("clave pública age inválida %q: %w", f, err)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, errors.New("falta al menos una clave pública age (age1…)")
	}
	return out, nil
}

func (s *Service) now() time.Time { return s.Clock.Now() }

func (s *Service) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run hace un respaldo completo y lo registra. trigger: "manual" o "scheduled"; userID: quién lo pidió.
// Devuelve el registro también cuando falla (con Status "failed").
func (s *Service) Run(ctx context.Context, trigger, userID string) (*store.BackupRun, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()

	run := &store.BackupRun{ID: auth.NewID(), Trigger: trigger, UserID: userID, StartedAt: s.now()}
	err := s.run(ctx, run)
	run.FinishedAt = s.now()
	run.Status = "ok"
	if err != nil {
		run.Status, run.Error = "failed", truncate(err.Error(), 300)
	}
	// El registro usa un contexto propio: si se canceló la petición, igual debe quedar constancia.
	rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ierr := s.Store.InsertBackupRun(rctx, run); ierr != nil {
		return run, errors.Join(err, ierr)
	}
	if err != nil {
		s.log().Error("respaldo externo fallido", "trigger", trigger, "intentos", run.Attempts, "err", run.Error)
		if s.Alert != nil {
			s.Alert(rctx, "backup-failed:"+run.FinishedAt.UTC().Format("2006-01-02"), "El respaldo cifrado no se pudo enviar al destino externo. Revísalo en Ajustes.")
		}
		return run, err
	}
	s.log().Info("respaldo externo listo", "trigger", trigger, "nombre", run.Name, "bytes", run.SizeBytes, "intentos", run.Attempts)
	return run, nil
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func truncate(v string, n int) string {
	if len(v) > n {
		return v[:n]
	}
	return v
}

func (s *Service) run(ctx context.Context, run *store.BackupRun) error {
	if s.Dest == nil || len(s.Recipients) == 0 {
		return errors.New("el respaldo externo no está configurado")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	work, err := os.MkdirTemp(s.Dir, ".trabajo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work) // la copia en claro nunca sobrevive a esta función
	plain := filepath.Join(work, "pulse.db")
	if err := db.Backup(ctx, s.DB, plain); err != nil {
		return fmt.Errorf("instantánea de la base: %w", err)
	}
	v, err := db.Version(ctx, s.DB)
	if err != nil {
		return err
	}
	run.Name = fmt.Sprintf("pulse-%s-v%d.db.age", run.StartedAt.UTC().Format("20060102T150405Z"), v)
	enc := filepath.Join(work, run.Name)
	if run.SizeBytes, run.SHA256, err = encryptFile(plain, enc, s.Recipients); err != nil {
		return fmt.Errorf("cifrando: %w", err)
	}
	if err := os.Remove(plain); err != nil {
		return err
	}
	if err := s.upload(ctx, run, enc); err != nil {
		return err
	}
	// Queda la última copia CIFRADA para descargarla desde Ajustes (rename: atómico).
	return os.Rename(enc, filepath.Join(s.Dir, LatestName))
}

// encryptFile cifra src en dst para los destinatarios y devuelve tamaño y SHA-256 del resultado.
func encryptFile(src, dst string, recipients []age.Recipient) (int64, string, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	counter := &countWriter{}
	w, err := age.Encrypt(io.MultiWriter(out, h, counter), recipients...)
	if err != nil {
		out.Close()
		return 0, "", err
	}
	if _, err := io.Copy(w, in); err != nil {
		out.Close()
		return 0, "", err
	}
	if err := w.Close(); err != nil {
		out.Close()
		return 0, "", err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return 0, "", err
	}
	return counter.n, hex.EncodeToString(h.Sum(nil)), out.Close()
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// upload sube con reintentos y espera exponencial; un error permanente (credenciales, permisos) no se reintenta.
func (s *Service) upload(ctx context.Context, run *store.BackupRun, path string) error {
	attempts, wait := s.Attempts, s.Backoff
	if attempts <= 0 {
		attempts = 4
	}
	if wait <= 0 {
		wait = 5 * time.Second
	}
	var last error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			if err := s.sleep(ctx, wait); err != nil {
				return errors.Join(last, err)
			}
			wait *= 2
		}
		run.Attempts++
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		last = s.Dest.Put(ctx, run.Name, f, run.SizeBytes, run.SHA256)
		f.Close()
		if last == nil {
			return nil
		}
		var perm *PermanentError
		if errors.As(last, &perm) || ctx.Err() != nil {
			break
		}
	}
	return fmt.Errorf("subiendo a %s: %w", s.Dest.Describe(), last)
}

// Status resume el estado para /healthz y Ajustes.
type Status struct {
	Configured bool              `json:"configured"`
	Target     string            `json:"target"`
	Interval   string            `json:"interval"`
	State      string            `json:"state"` // off | ok | stale | failing | pending
	LastOK     *store.BackupRun  `json:"last_ok"`
	Runs       []store.BackupRun `json:"runs"`
	Running    bool              `json:"running"`
	Download   bool              `json:"download"` // hay una copia cifrada para descargar
}

// StaleAfter: sin un respaldo correcto en este tiempo, el estado es "stale".
func (s *Service) StaleAfter() time.Duration {
	if s.Interval > 0 {
		return 2*s.Interval + time.Hour
	}
	return 8 * 24 * time.Hour // solo manual: avisa si pasa más de una semana
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	st := &Status{Configured: s != nil && s.Dest != nil, State: "off", Runs: []store.BackupRun{}}
	if !st.Configured {
		return st, nil
	}
	st.Target = s.Dest.Describe()
	st.Interval = "manual"
	if s.Interval > 0 {
		st.Interval = s.Interval.String()
	}
	s.mu.Lock()
	st.Running = s.running
	s.mu.Unlock()
	runs, err := s.Store.BackupRuns(ctx, 10)
	if err != nil {
		return nil, err
	}
	st.Runs = runs
	last, err := s.Store.LastBackup(ctx, "ok")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	st.LastOK = last
	if _, err := os.Stat(filepath.Join(s.Dir, LatestName)); err == nil {
		st.Download = true
	}
	switch {
	case len(runs) == 0:
		st.State = "pending"
	case runs[0].Status == "failed":
		st.State = "failing"
	case last != nil && s.now().Sub(last.FinishedAt) > s.StaleAfter():
		st.State = "stale"
	default:
		st.State = "ok"
	}
	return st, nil
}

// due dice si toca un respaldo programado: nunca hubo uno, o el último intento terminó hace más de
// Interval (si falló, se reintenta a la hora).
func (s *Service) due(ctx context.Context) (bool, error) {
	last, err := s.Store.LastBackup(ctx, "")
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	every := s.Interval
	if last.Status == "failed" && every > time.Hour {
		every = time.Hour
	}
	return s.now().Sub(last.FinishedAt) >= every, nil
}

// Tick hace el respaldo programado si toca y avisa una vez al día si el último correcto es muy viejo.
func (s *Service) Tick(ctx context.Context) {
	if s.Interval > 0 {
		if ok, err := s.due(ctx); err != nil {
			s.log().Error("respaldo programado", "err", err.Error())
		} else if ok {
			s.Run(ctx, "scheduled", "")
		}
	}
	st, err := s.Status(ctx)
	if err == nil && st.State == "stale" && s.Alert != nil {
		s.Alert(ctx, "backup-stale:"+s.now().UTC().Format("2006-01-02"), "Hace demasiado que no hay un respaldo cifrado correcto. Revísalo en Ajustes.")
	}
}

// Loop revisa cada minuto; el primer respaldo programado espera a que el servidor lleve un rato arriba.
func (s *Service) Loop(ctx context.Context) {
	if err := s.sleep(ctx, 2*time.Minute); err != nil {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Decrypt descifra un respaldo con una identidad age (archivo de age-keygen) y verifica la huella si se da.
func Decrypt(src, dst string, identities []age.Identity, wantSHA256 string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if wantSHA256 != "" {
		h := sha256.New()
		if _, err := io.Copy(h, in); err != nil {
			return err
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA256) {
			return fmt.Errorf("la huella del respaldo no coincide (%s): no lo restaures", got)
		}
		if _, err := in.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	r, err := age.Decrypt(in, identities...)
	if err != nil {
		return fmt.Errorf("no se pudo descifrar (¿identidad equivocada o archivo alterado?): %w", err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("el respaldo está dañado o fue alterado: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

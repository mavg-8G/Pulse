package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"pulse/internal/backup"
	"pulse/internal/push"
)

// ---------- respaldo externo cifrado (fase 12) ----------

func (s *Server) requireAdmin(r *http.Request) error {
	if ident(r).user.Role != "admin" {
		return errf(403, "forbidden", "Solo un administrador puede ver esto.")
	}
	return nil
}

// GET /api/admin/backups: estado, destino (sin credenciales) e historial.
func (s *Server) adminBackups(w http.ResponseWriter, r *http.Request) error {
	if err := s.requireAdmin(r); err != nil {
		return err
	}
	st, err := s.backup.Status(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, 200, st)
	return nil
}

// POST /api/admin/backups: "Respaldar ahora". Solo admin con step-up; queda en la actividad de seguridad.
func (s *Server) adminBackupNow(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	if s.backup == nil {
		return errf(409, "backup_off", "El respaldo externo no está configurado en el servidor.")
	}
	// Respaldar y subir puede tardar más que el límite general de escritura.
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	run, err := s.backup.Run(r.Context(), "manual", id.user.ID)
	if errors.Is(err, backup.ErrBusy) {
		return errf(409, "backup_busy", "Ya hay un respaldo en curso. Espera a que termine.")
	}
	if run == nil {
		return err
	}
	meta := map[string]string{"result": run.Status, "name": run.Name, "size": strconv.FormatInt(run.SizeBytes, 10), "attempts": strconv.Itoa(run.Attempts)}
	s.audit(r, id.user.ID, "backup_manual", meta)
	writeJSON(w, 200, run) // también si falló: el registro dice por qué
	return nil
}

// GET /api/admin/backups/latest: descarga la última copia CIFRADA (sin la identidad age no se puede leer).
func (s *Server) adminBackupDownload(w http.ResponseWriter, r *http.Request) error {
	id := ident(r)
	if s.backup == nil {
		return errNotFound
	}
	f, err := os.Open(filepath.Join(s.backup.Dir, backup.LatestName))
	if errors.Is(err, os.ErrNotExist) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	s.audit(r, id.user.ID, "backup_downloaded", map[string]string{"size": strconv.FormatInt(fi.Size(), 10)})
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pulse-%s.db.age"`, fi.ModTime().UTC().Format("20060102T150405Z")))
	http.ServeContent(w, r, "", fi.ModTime(), f)
	return nil
}

// NotifyAdmins avisa por push a los administradores activos. key evita repetir el aviso.
func (s *Server) NotifyAdmins(ctx context.Context, key, title, body, url string) {
	if s.push == nil {
		return
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.log.Error("avisando a administradores", "err", err.Error())
		return
	}
	for _, a := range users {
		if a.Role != "admin" || a.DisabledAt != nil {
			continue
		}
		if err := s.push.Notify(ctx, a.ID, key, "security", push.Payload{Title: title, Body: body, URL: url}); err != nil {
			s.log.Error("avisando a administradores", "err", err.Error())
		}
	}
}

// BackupAlert es el aviso que usa el servicio de respaldos cuando falla o se queda viejo.
func (s *Server) BackupAlert(ctx context.Context, key, body string) {
	s.NotifyAdmins(ctx, key, "Respaldo de Pulse", body, "/ajustes")
}

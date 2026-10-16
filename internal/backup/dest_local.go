package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ---------------- Carpeta del propio servidor ----------------

// Local guarda el respaldo cifrado en una carpeta del servidor (p. ej. /opt/backup/pulse montada en /backups)
// y conserva solo los Keep más recientes. Sirve para volver atrás rápido; no protege si se pierde el VPS,
// para eso está el destino externo.
type Local struct {
	Dir  string
	Keep int // respaldos a conservar; 0 = todos
}

func (l *Local) Describe() string { return "Este servidor · " + l.Dir }

func (l *Local) Put(ctx context.Context, name string, body io.ReadSeeker, size int64, hexSum string) error {
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return &PermanentError{fmt.Errorf("nombre de respaldo inválido %q", name)}
	}
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return &PermanentError{err}
	}
	tmp, err := os.CreateTemp(l.Dir, ".subiendo-*")
	if err != nil {
		return &PermanentError{err}
	}
	defer os.Remove(tmp.Name()) // no-op tras el rename
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), body)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != hexSum {
		return errors.New("la copia local no coincide con el respaldo cifrado")
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(l.Dir, name)); err != nil {
		return err
	}
	return l.prune()
}

// prune borra los respaldos más viejos. Los nombres llevan la fecha UTC, así que el orden alfabético es el cronológico.
func (l *Local) prune() error {
	if l.Keep <= 0 {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(l.Dir, "pulse-*.db.age"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for len(files) > l.Keep {
		if err := os.Remove(files[0]); err != nil {
			return err
		}
		files = files[1:]
	}
	return nil
}

// ---------------- Varios destinos a la vez ----------------

// Multi envía el mismo respaldo a todos sus destinos. Si uno falla, los reintentos solo repiten los que faltan.
// El error es permanente solo si todos los que fallaron fallaron de forma permanente.
type Multi struct {
	Dests []Destination

	mu   sync.Mutex
	name string       // respaldo en curso
	done map[int]bool // destinos que ya lo tienen
}

func (m *Multi) Describe() string {
	parts := make([]string, len(m.Dests))
	for i, d := range m.Dests {
		parts[i] = d.Describe()
	}
	return strings.Join(parts, " + ")
}

func (m *Multi) Put(ctx context.Context, name string, body io.ReadSeeker, size int64, hexSum string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.name != name {
		m.name, m.done = name, map[int]bool{}
	}
	var errs []error
	allPerm := true
	for i, d := range m.Dests {
		if m.done[i] {
			continue
		}
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := d.Put(ctx, name, body, size, hexSum); err != nil {
			var perm *PermanentError
			allPerm = allPerm && errors.As(err, &perm)
			errs = append(errs, fmt.Errorf("%s: %w", d.Describe(), err))
			continue
		}
		m.done[i] = true
	}
	if len(errs) == 0 {
		return nil
	}
	err := errors.Join(errs...)
	if allPerm {
		return &PermanentError{err}
	}
	return err
}

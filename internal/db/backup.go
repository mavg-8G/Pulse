package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Backup copia la base en caliente a dest con VACUUM INTO: instantánea consistente aunque
// el servidor esté escribiendo, sin el WAL pendiente y compactada. dest no debe existir.
func Backup(ctx context.Context, db *sql.DB, dest string) error {
	if strings.ContainsAny(dest, "'\x00") {
		return errors.New("ruta de respaldo inválida")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s ya existe", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return err
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return err
	}
	return Check(ctx, dest)
}

// Check comprueba que path es una base de Pulse íntegra y devuelve error si no.
func Check(ctx context.Context, path string) error {
	d, err := sql.Open("sqlite3", "file:"+path+"?mode=ro&_foreign_keys=on")
	if err != nil {
		return err
	}
	defer d.Close()
	var res string
	if err := d.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return fmt.Errorf("%s no es una base SQLite válida: %w", path, err)
	}
	if res != "ok" {
		return fmt.Errorf("integridad de %s: %s", path, res)
	}
	var fk int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fk); err != nil || fk != 0 {
		return fmt.Errorf("%s tiene claves foráneas rotas", path)
	}
	var v sql.NullInt64
	if err := d.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil || v.Int64 < 1 {
		return fmt.Errorf("%s no parece una base de Pulse", path)
	}
	return nil
}

// Restore reemplaza la base dbPath por el respaldo src. El servidor debe estar detenido.
// Verifica el respaldo antes de tocar nada y el reemplazo es atómico (rename).
func Restore(ctx context.Context, src, dbPath string) error {
	if err := Check(ctx, src); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dbPath + ".restaurando"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	// El WAL de la base anterior no corresponde al respaldo: se descarta.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, dbPath)
}

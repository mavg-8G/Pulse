package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRespaldoYRestauracion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "pulse.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, password_hash, role, dek_enc, timezone, created_at) VALUES ('u1', 'a@b.c', 'x', 'user', x'00', 'UTC', 'x')`); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backups", "antes.db")
	if err := Backup(ctx, d, backup); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(backup); fi.Mode().Perm() != 0o600 {
		t.Fatalf("permisos del respaldo: %v", fi.Mode().Perm())
	}
	if err := Backup(ctx, d, backup); err == nil {
		t.Fatal("no debe sobrescribir un respaldo existente")
	}
	// Cambios posteriores (como los de una migración nueva) …
	if _, err := d.Exec(`DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	d.Close()
	// … se deshacen al restaurar.
	if err := Restore(ctx, backup, path); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Fatalf("usuarios tras restaurar: %d", n)
	}
	// Un archivo que no es una base de Pulse no se restaura y no toca nada.
	junk := filepath.Join(dir, "basura.db")
	os.WriteFile(junk, []byte("no soy sqlite"), 0o600)
	if err := Restore(ctx, junk, path); err == nil {
		t.Fatal("debe rechazar un respaldo inválido")
	}
	other := filepath.Join(dir, "otra.db")
	o, _ := Open(other)
	o.Exec(`CREATE TABLE x (a)`)
	o.Close()
	if err := Restore(ctx, other, path); err == nil {
		t.Fatal("debe rechazar una base que no es de Pulse")
	}
	d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n != 1 {
		t.Fatal("un intento fallido no debe tocar la base")
	}
}

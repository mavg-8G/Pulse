// Package db abre SQLite con los PRAGMA de seguridad y aplica las migraciones embebidas.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open abre (o crea) la base de datos. Cada conexión activa claves foráneas,
// WAL, borrado seguro y transacciones IMMEDIATE (evita bloqueos de escritura tardíos).
func Open(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Set("_foreign_keys", "on")
	q.Set("_journal_mode", "WAL")
	q.Set("_busy_timeout", "5000")
	q.Set("_secure_delete", "on")
	q.Set("_synchronous", "NORMAL")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite3", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("abriendo %s: %w", path, err)
	}
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		db.Close()
		return nil, fmt.Errorf("no se pudieron activar las claves foráneas")
	}
	return db, nil
}

type migration struct {
	version  int
	name     string
	up, down string
}

func loadMigrations() ([]migration, error) {
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, f := range files {
		base := strings.TrimPrefix(f, "migrations/")
		v, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("migración con nombre inválido: %s", base)
		}
		b, _ := migrationsFS.ReadFile(f)
		s := string(b)
		ui, di := strings.Index(s, "-- +up"), strings.Index(s, "-- +down")
		if ui < 0 || di < ui {
			return nil, fmt.Errorf("%s debe tener secciones -- +up y -- +down", base)
		}
		ms = append(ms, migration{version: v, name: base, up: s[ui+6 : di], down: s[di+8:]})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i := range ms {
		if ms[i].version != i+1 {
			return nil, fmt.Errorf("numeración de migraciones con huecos en %s", ms[i].name)
		}
	}
	return ms, nil
}

func ensureTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`)
	return err
}

// Latest devuelve la versión de esquema que espera este binario.
func Latest() (int, error) {
	ms, err := loadMigrations()
	return len(ms), err
}

// Version devuelve la versión de esquema aplicada.
func Version(ctx context.Context, db *sql.DB) (int, error) {
	if err := ensureTable(ctx, db); err != nil {
		return 0, err
	}
	var v sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

// Migrate aplica todas las migraciones pendientes, cada una en su transacción.
func Migrate(ctx context.Context, db *sql.DB) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	cur, err := Version(ctx, db)
	if err != nil {
		return err
	}
	if cur > len(ms) {
		return fmt.Errorf("la base está en la versión %d pero este binario solo conoce hasta la %d", cur, len(ms))
	}
	for _, m := range ms[cur:] {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.up); err != nil {
			tx.Rollback()
			return fmt.Errorf("migración %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// MigrateDown revierte hasta dejar la base en la versión target.
func MigrateDown(ctx context.Context, db *sql.DB, target int) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	cur, err := Version(ctx, db)
	if err != nil {
		return err
	}
	for v := cur; v > target; v-- {
		m := ms[v-1]
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.down); err != nil {
			tx.Rollback()
			return fmt.Errorf("revirtiendo %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

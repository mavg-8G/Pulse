package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"time"
)

func rotateKeysCmd(args []string) error {
	f := flag.NewFlagSet("rotate-keys", flag.ContinueOnError)
	outDB := f.String("out-db", "", "nueva copia de la base")
	outKey := f.String("out-key", "", "nuevo llavero secreto")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *outDB == "" || *outKey == "" {
		return errors.New("uso: rotate-keys --out-db copia.db --out-key llavero.json")
	}
	return withApp(func(a *app) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		next, raw, err := a.keys.RotateBundle()
		if err != nil {
			return err
		}
		defer clear(raw)
		keyFile, err := os.OpenFile(*outKey, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := keyFile.Write(raw)
		if writeErr == nil {
			writeErr = keyFile.Sync()
		}
		closeErr := keyFile.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := db.Backup(ctx, a.store.DB, *outDB); err != nil {
			return err
		}
		copyDB, err := db.Open(*outDB)
		if err != nil {
			return err
		}
		defer copyDB.Close()
		if err := rotateDatabase(ctx, copyDB, a.keys, next); err != nil {
			return fmt.Errorf("rotación incompleta; no activar los archivos de salida: %w", err)
		}
		if _, err := copyDB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return err
		}
		fmt.Println("Copia y llavero preparados. Detén el servicio y valida la pareja en un entorno aislado antes de activarla. El origen no ha sido sustituido.")
		return nil
	})
}

func rotateDatabase(ctx context.Context, d *sql.DB, old, next *crypto.Keyring) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id, dek_enc FROM users`)
	if err != nil {
		return err
	}
	type wrapped struct {
		id   string
		blob []byte
	}
	var users []wrapped
	for rows.Next() {
		var u wrapped
		if err := rows.Scan(&u.id, &u.blob); err != nil {
			rows.Close()
			return err
		}
		u.blob, err = old.RewrapDEK(u.id, u.blob, next)
		if err != nil {
			rows.Close()
			return err
		}
		users = append(users, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, u := range users {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET dek_enc = ? WHERE id = ?`, u.blob, u.id); err != nil {
			return err
		}
	}
	// All encrypted server settings are explicit; never try decrypting migration markers.
	var google []byte
	err = tx.QueryRowContext(ctx, `SELECT value FROM server_settings WHERE key = 'google'`).Scan(&google)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		plain, err := old.Server().OpenBytes("google", google)
		if err != nil {
			return err
		}
		defer clear(plain)
		if _, err := tx.ExecContext(ctx, `UPDATE server_settings SET value = ? WHERE key = 'google'`, next.Server().SealBytes("google", plain)); err != nil {
			return err
		}
	}
	// A rotated installation starts without pre-existing browser sessions/challenges.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions`); err != nil {
		return err
	}
	return tx.Commit()
}

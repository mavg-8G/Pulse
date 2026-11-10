package store

import (
	"context"
	"database/sql"
	"fmt"
	"pulse/internal/crypto"
)

const rowEncryptionMarker = "security.row-encryption.v2"

// MigrateRowEncryption runs before serving requests or starting background workers.
// Batches are transactional and resumable; normal reads NEVER accept the old format.
// All SQL identifiers below are fixed constants, never request data.
func (s *Store) MigrateRowEncryption(ctx context.Context, keys *crypto.Keyring) error {
	if b, err := s.Setting(ctx, rowEncryptionMarker); err == nil && string(b) == "done" {
		return nil
	} else if err != nil && err != ErrNotFound {
		return err
	}
	for _, spec := range []struct{ table, column, field string }{
		{"transactions", "merchant_enc", "tx.merchant"},
		{"transactions", "description_enc", "tx.description"},
		{"transactions", "notes_enc", "tx.notes"},
		{"recurring", "merchant_enc", "recurring.merchant"},
		{"template_alerts", "subject_enc", "template.subject"},
	} {
		var after int64
		for {
			count := 0
			err := s.Tx(ctx, func(tx *sql.Tx) error {
				rows, err := tx.QueryContext(ctx, `SELECT t.rowid, t.id, t.user_id, t.`+spec.column+`, u.dek_enc FROM `+spec.table+` t JOIN users u ON u.id = t.user_id WHERE t.rowid > ? ORDER BY t.rowid LIMIT 100`, after)
				if err != nil {
					return err
				}
				type change struct {
					row  int64
					blob []byte
				}
				changes := []change{}
				for rows.Next() {
					var row int64
					var id, uid string
					var blob, dek []byte
					if err := rows.Scan(&row, &id, &uid, &blob, &dek); err != nil {
						rows.Close()
						return err
					}
					sl, err := keys.Sealer(uid, dek)
					if err != nil {
						rows.Close()
						return err
					}
					bound, err := sl.RebindRecord(id, spec.field, blob)
					if err != nil {
						rows.Close()
						return fmt.Errorf("migrando cifrado %s: %w", spec.table, err)
					}
					changes = append(changes, change{row, bound})
					after = row
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return err
				}
				for _, c := range changes {
					if _, err := tx.ExecContext(ctx, `UPDATE `+spec.table+` SET `+spec.column+` = ? WHERE rowid = ?`, c.blob, c.row); err != nil {
						return err
					}
				}
				count = len(changes)
				return nil
			})
			if err != nil {
				return err
			}
			if count < 100 {
				break
			}
		}
	}
	return s.SetSetting(ctx, rowEncryptionMarker, []byte("done"))
}

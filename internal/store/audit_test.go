package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pulse/internal/clock"
	"pulse/internal/db"
	"pulse/internal/store"
)

func TestRetencionAuditoria(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	// El trigger de la base compara con la hora real: el reloj falso arranca justo antes de la retención.
	c := clock.NewFake(time.Now().Add(-store.AuditRetention - 2*time.Hour))
	st := &store.Store{DB: d, Clock: c}
	st.Audit(ctx, "", "viejo", "203.0.113.1", "ua", nil)
	c.Advance(store.AuditRetention - time.Hour)
	st.Audit(ctx, "", "reciente", "203.0.113.2", "ua", nil)
	c.Advance(2 * time.Hour) // "viejo" ya superó la retención; "reciente" no
	if err := st.PurgeExpiredSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var events []string
	rows, err := d.Query(`SELECT event FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		rows.Scan(&e)
		events = append(events, e)
	}
	if len(events) != 1 || events[0] != "reciente" {
		t.Fatalf("tras la purga quedan %v, esperaba [reciente]", events)
	}
	// Fuera de la purga por retención, el registro sigue siendo inalterable.
	if _, err := d.Exec(`DELETE FROM audit_log`); err == nil {
		t.Fatal("se pudo borrar una entrada reciente del registro de seguridad")
	}
	if _, err := d.Exec(`UPDATE audit_log SET event = 'x'`); err == nil {
		t.Fatal("se pudo modificar el registro de seguridad")
	}
}

package store_test

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pulse/internal/clock"
	"pulse/internal/db"
	"pulse/internal/store"
)

func TestAuditFailureIsObservableAndDoesNotLogSecrets(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	var log bytes.Buffer
	st := &store.Store{DB: d, Clock: clock.Real{}, Log: slog.New(slog.NewJSONHandler(&log, nil))}
	before := store.AuditFailures.Load()
	st.Audit(context.Background(), "u", "password_changed", "private-ip", "private-agent", map[string]string{"secret": "must-not-appear"})
	if store.AuditFailures.Load() != before+1 || !strings.Contains(log.String(), "audit_write_failed") {
		t.Fatal("fallo de auditoría invisible")
	}
	for _, secret := range []string{"private-ip", "private-agent", "must-not-appear"} {
		if strings.Contains(log.String(), secret) {
			t.Fatal("secreto en logs")
		}
	}
}

func TestAuditSurvivesClientCancellation(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.Migrate(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	st := &store.Store{DB: d, Clock: clock.Real{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st.Audit(ctx, "", "cancelled_request", "", "", nil)
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE event = 'cancelled_request'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

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

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"pulse/internal/auth"
	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/store"
	"testing"
	"time"
)

func TestDatabaseRotationPreservesDataAndInvalidatesSessions(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "rotation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	old, _ := crypto.NewKeyring(bytes.Repeat([]byte{7}, 32))
	next, _, err := old.RotateBundle()
	if err != nil {
		t.Fatal(err)
	}
	dek := old.NewDEK("u")
	if _, err := d.Exec(`INSERT INTO users(id,email,password_hash,role,dek_enc,timezone,created_at) VALUES('u','u@example.com','x','user',?,'UTC','2026-01-01T00:00:00Z')`, dek); err != nil {
		t.Fatal(err)
	}
	st := &store.Store{DB: d, Clock: clock.Real{}}
	if err := st.SetSetting(ctx, "google", old.Server().SealBytes("google", []byte("credentials"))); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "security.row-encryption.v2", []byte("done")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := st.CreateSession(ctx, &store.Session{ID: auth.NewID(), UserID: "u", State: "full", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour)}, old.Lookup("session", []byte("token"))); err != nil {
		t.Fatal(err)
	}
	if err := rotateDatabase(ctx, d, old, next); err != nil {
		t.Fatal(err)
	}
	u, err := st.User(ctx, "u")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Sealer("u", u.DEKEnc); err == nil {
		t.Fatal("clave anterior abre la base rotada")
	}
	if _, err := next.Sealer("u", u.DEKEnc); err != nil {
		t.Fatal(err)
	}
	b, err := st.Setting(ctx, "google")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := next.Server().OpenBytes("google", b); err != nil || string(got) != "credentials" {
		t.Fatal(string(got), err)
	}
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("sesiones no revocadas", count, err)
	}
}

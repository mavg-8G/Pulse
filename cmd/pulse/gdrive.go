package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"pulse/internal/backup"
)

// gdriveAuthCmd autoriza una vez los respaldos en Google Drive. Se puede correr en el servidor con un túnel SSH
// (ver deploy/README.md): Google redirige a http://127.0.0.1:<puerto>/ en el navegador de la persona.
func gdriveAuthCmd(args []string) error {
	fs := flag.NewFlagSet("gdrive-auth", flag.ContinueOnError)
	clientID := fs.String("client-id", os.Getenv("PULSE_BACKUP_GDRIVE_CLIENT_ID"), "ID del cliente OAuth (tipo app de escritorio)")
	secretFile := fs.String("client-secret-file", os.Getenv("PULSE_BACKUP_GDRIVE_CLIENT_SECRET_FILE"), "archivo con el secreto del cliente OAuth")
	out := fs.String("out", "", "archivo donde guardar el refresh token (no debe existir)")
	listen := fs.String("listen", "127.0.0.1:53682", "dirección local donde esperar la respuesta de Google")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *clientID == "" || *secretFile == "" || *out == "" {
		return errors.New("uso: pulse gdrive-auth --client-id ID --client-secret-file archivo --out archivo [--listen 127.0.0.1:53682]")
	}
	secret, err := readSecret(*secretFile)
	if err != nil {
		return fmt.Errorf("--client-secret-file: %w", err)
	}
	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	token, err := backup.GDriveAuthorize(ctx, backup.GoogleDriveEndpoints(), *clientID, secret, *listen, func(u string) {
		fmt.Printf("Abre esta dirección en tu navegador y acepta el acceso a Google Drive:\n\n%s\n\nEsperando a Google…\n", u)
	})
	if err == nil {
		_, err = fmt.Fprintln(f, token)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(*out)
		return err
	}
	fmt.Printf("✓ Permiso guardado en %s. Pulse solo puede ver y tocar los archivos que él mismo cree en tu Drive.\n", *out)
	return nil
}

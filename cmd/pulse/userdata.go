package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"pulse/internal/store"
)

// Derechos de la persona (LOPDP de Ecuador y RGPD): acceso/portabilidad y supresión.
// Los ejecuta el administrador por CLI cuando alguien lo pide (ver /privacidad).

type exportRule struct {
	Pattern    string    `json:"patron"`
	CategoryID string    `json:"category_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type userExport struct {
	GeneratedAt time.Time `json:"generado"`
	Format      string    `json:"formato"`
	Profile     struct {
		Email     string    `json:"email"`
		Role      string    `json:"rol"`
		Timezone  string    `json:"zona_horaria"`
		Theme     string    `json:"tema"`
		CreatedAt time.Time `json:"creado"`
	} `json:"perfil"`
	Accounts       []store.Account     `json:"cuentas"`
	Categories     []store.Category    `json:"categorias"`
	Budgets        []store.Budget      `json:"presupuestos"`
	Transactions   []store.Transaction `json:"movimientos"`
	Rules          []exportRule        `json:"reglas_de_comercio"`
	GmailConnected bool                `json:"gmail_conectado"`
	Security       []store.AuditEntry  `json:"actividad_de_seguridad"`
}

// exportUser escribe en `out` (archivo nuevo, permisos 0600) todos los datos de la persona, descifrados.
func exportUser(ctx context.Context, a *app, email, out string) error {
	u, err := a.store.UserByEmail(ctx, email)
	if err != nil {
		return errors.New("usuario no encontrado")
	}
	sl, err := a.keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		return err
	}
	var x userExport
	x.GeneratedAt = time.Now().UTC()
	x.Format = "pulse-export/1"
	x.Profile.Email, x.Profile.Role, x.Profile.Timezone, x.Profile.Theme, x.Profile.CreatedAt = u.Email, u.Role, u.Timezone, u.Theme, u.CreatedAt
	if x.Accounts, err = a.store.Accounts(ctx, u.ID, true); err != nil {
		return err
	}
	if x.Categories, err = a.store.Categories(ctx, u.ID); err != nil {
		return err
	}
	if x.Budgets, err = a.store.Budgets(ctx, u.ID); err != nil {
		return err
	}
	if x.Transactions, err = a.store.Transactions(ctx, u.ID, sl, store.TxFilter{}); err != nil {
		return err
	}
	rules, err := a.store.Rules(ctx, u.ID)
	if err != nil {
		return err
	}
	x.Rules = []exportRule{}
	for _, r := range rules {
		p, err := sl.Open("rule.pattern", r.PatternEnc)
		if err != nil {
			return err
		}
		x.Rules = append(x.Rules, exportRule{Pattern: p, CategoryID: r.CategoryID, CreatedAt: r.CreatedAt})
	}
	if _, err := a.store.Gmail(ctx, u.ID); err == nil {
		x.GmailConnected = true
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if x.Security, err = a.store.AuditFor(ctx, u.ID, 100000); err != nil {
		return err
	}
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(x); err != nil {
		f.Close()
		os.Remove(out)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	a.store.Audit(ctx, u.ID, "admin_export", "cli", "", nil)
	fmt.Printf("Exportado a %s (%d movimientos). Contiene datos personales en claro: entrégalo por un canal seguro y bórralo después.\n", out, len(x.Transactions))
	return nil
}

// deleteUser borra la cuenta y todos sus datos. Exige repetir el email como confirmación.
func deleteUser(ctx context.Context, a *app, email, confirm string) error {
	u, err := a.store.UserByEmail(ctx, email)
	if err != nil {
		return errors.New("usuario no encontrado")
	}
	if confirm != u.Email {
		return fmt.Errorf("para confirmar repite el email: pulse user delete %s --confirmar %s", u.Email, u.Email)
	}
	_, gmailErr := a.store.Gmail(ctx, u.ID)
	if err := a.store.DeleteUser(ctx, u.ID); err != nil {
		return err
	}
	a.store.Audit(ctx, u.ID, "admin_delete", "cli", "", nil)
	fmt.Println("Cuenta y datos eliminados. Los respaldos que la contengan se borran al rotar (se guardan los últimos 15).")
	if gmailErr == nil {
		fmt.Println("Tenía Gmail conectado: el token ya no existe en Pulse; la persona puede quitar el acceso en https://myaccount.google.com/permissions")
	}
	return nil
}

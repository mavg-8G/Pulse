// Package admin agrupa las operaciones que solo se hacen desde la línea de comandos
// del servidor (no existen endpoints web de administración).
package admin

import (
	"context"
	"fmt"
	"time"

	"pulse/internal/auth"
	"pulse/internal/crypto"
	"pulse/internal/store"
)

// CreateInvite genera un código de un solo uso. El código se muestra una vez; en la BD queda su HMAC.
func CreateInvite(ctx context.Context, st *store.Store, keys *crypto.Keyring, email, role string, ttl time.Duration) (code, id string, err error) {
	if role != "user" && role != "admin" {
		return "", "", fmt.Errorf("rol inválido %q", role)
	}
	if ttl < time.Minute || ttl > 30*24*time.Hour {
		return "", "", fmt.Errorf("la vigencia debe estar entre 1 minuto y 30 días")
	}
	code = auth.InviteCode()
	id = auth.NewID()
	err = st.CreateInvite(ctx, id, keys.Lookup("invite", []byte(auth.NormalizeCode(code))), email, role, st.Clock.Now().Add(ttl))
	return code, id, err
}

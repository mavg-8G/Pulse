package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"pulse/internal/auth"
	"pulse/internal/budget"
	"pulse/internal/crypto"
	"pulse/internal/store"
)

type Payload struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	URL         string `json:"url"`
	Tag         string `json:"tag"`
	UserID      string `json:"user_id"`
	PrivateBody string `json:"-"`
}

type Service struct {
	Store  *store.Store
	Keys   *crypto.Keyring
	Sender Sender
	Log    *slog.Logger
	mu     sync.Mutex
}

func QuietNow(p store.NotificationPreferences, now time.Time) bool {
	if !p.QuietEnabled {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	if p.QuietStart < p.QuietEnd {
		return minute >= p.QuietStart && minute < p.QuietEnd
	}
	return minute >= p.QuietStart || minute < p.QuietEnd
}

func enabled(p store.NotificationPreferences, kind string) bool {
	switch kind {
	case "budget":
		return p.Budgets
	case "security":
		return p.Security
	case "weekly":
		return p.Weekly
	case "monthly":
		return p.Monthly
	case "import":
		return p.Budgets // los gastos detectados en Gmail van con las alertas de gastos
	default:
		return true
	}
}

// Tick es determinista con Store.Clock y se serializa para no duplicar envíos.
func (s *Service) Tick(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	users, err := s.Store.ListUsers(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, u := range users {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if u.DisabledAt != nil {
			continue
		}
		if err := s.tickUser(ctx, u); err != nil {
			failures = append(failures, err)
			if s.Log != nil {
				s.Log.Error("procesando notificaciones", "user", u.ID, "err", err.Error())
			}
			// Continúa con otros usuarios aunque un envío falle.
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	return errors.Join(append(failures, s.Store.PrunePush(ctx))...)
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil && s.Log != nil {
			s.Log.Error("notificaciones", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) tickUser(ctx context.Context, u store.User) error {
	subs, err := s.Store.PushSubscriptions(ctx, u.ID)
	if err != nil || len(subs) == 0 {
		return err
	}
	p, err := s.Store.NotificationPrefs(ctx, u.ID)
	if err != nil {
		return err
	}
	sl, err := s.Keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return err
	}
	now := s.Store.Clock.Now().In(loc)
	if err := s.schedule(ctx, u, subs, p, sl, now); err != nil {
		return err
	}
	if QuietNow(p, now) {
		return nil
	}
	due, err := s.Store.DuePush(ctx, u.ID)
	if err != nil {
		return err
	}
	byID := map[string]store.PushSubscription{}
	for _, sub := range subs {
		byID[sub.ID] = sub
	}
	for _, d := range due {
		sub, ok := byID[d.SubscriptionID]
		if !ok {
			continue
		}
		if !enabled(p, d.Kind) {
			if err := s.Store.CompletePush(ctx, u.ID, d.ID); err != nil {
				return err
			}
			continue
		}
		raw, err := sl.OpenBytes("push.subscription", sub.Enc)
		if err != nil {
			return err
		}
		var target Subscription
		if err := json.Unmarshal(raw, &target); err != nil {
			return err
		}
		raw, err = sl.OpenBytes("push.payload", d.Enc)
		if err != nil {
			return err
		}
		// Se evalúa la privacidad al enviar, incluso si cambió mientras estaba en cola.
		var envelope struct {
			Payload     Payload `json:"payload"`
			PrivateBody string  `json:"private_body"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return err
		}
		if !p.HideAmounts && envelope.PrivateBody != "" {
			envelope.Payload.Body = envelope.PrivateBody
		}
		payload, err := json.Marshal(envelope.Payload)
		if err != nil {
			return err
		}
		result, sendErr := s.Sender.Send(ctx, target, payload)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch {
		case sendErr == nil && (result.Status == 404 || result.Status == 410):
			if err := s.Store.DeletePushSubscription(ctx, u.ID, sub.ID); err != nil {
				return err
			}
			delete(byID, sub.ID)
		case sendErr == nil && result.Status >= 200 && result.Status < 300:
			if err := s.Store.CompletePush(ctx, u.ID, d.ID); err != nil {
				return err
			}
		case sendErr != nil || result.Status == 429 || result.Status >= 500:
			delay := time.Minute * time.Duration(1<<min(d.Attempts, 8))
			if result.RetryAfter > delay {
				delay = result.RetryAfter
			}
			if delay > 24*time.Hour {
				delay = 24 * time.Hour
			}
			if err := s.Store.RetryPush(ctx, u.ID, d.ID, s.Store.Clock.Now().Add(delay)); err != nil {
				return err
			}
		default:
			// Los errores permanentes no se reintentan indefinidamente.
			if err := s.Store.CompletePush(ctx, u.ID, d.ID); err != nil {
				return err
			}
			if s.Log != nil {
				s.Log.Warn("push rechazado", "status", result.Status)
			}
		}
	}
	return nil
}

func (s *Service) queue(ctx context.Context, subs []store.PushSubscription, sl *crypto.Sealer, key, kind string, p Payload, after time.Time) error {
	for _, sub := range subs {
		if !after.IsZero() && sub.CreatedAt.After(after) {
			continue
		}
		p.Tag = key
		p.UserID = sub.UserID
		raw, err := json.Marshal(map[string]any{"payload": p, "private_body": p.PrivateBody})
		if err != nil {
			return err
		}
		if err := s.Store.QueuePush(ctx, sub, auth.NewID(), key, kind, sl.SealBytes("push.payload", raw)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) schedule(ctx context.Context, u store.User, subs []store.PushSubscription, p store.NotificationPreferences, sl *crypto.Sealer, now time.Time) error {
	// Escanea eventos recientes también tras reinicios; UNIQUE evita volver a enviarlos.
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT a.budget_id,a.month,a.threshold,a.spent_cents,a.created_at FROM budget_alerts a JOIN budgets b ON b.id=a.budget_id WHERE b.user_id=? AND a.created_at>=?`, u.ID, store.TS(s.Store.Clock.Now().Add(-48*time.Hour)))
	if err != nil {
		return err
	}
	type alert struct {
		id, month string
		threshold int
		spent     int64
		at        time.Time
	}
	var alerts []alert
	for rows.Next() {
		var a alert
		var ts string
		if err := rows.Scan(&a.id, &a.month, &a.threshold, &a.spent, &ts); err != nil {
			rows.Close()
			return err
		}
		a.at = store.ParseTS(ts)
		alerts = append(alerts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range alerts {
		if !p.Budgets {
			continue
		}
		key := fmt.Sprintf("budget:%s:%s:%d", a.id, a.month, a.threshold)
		payload := Payload{Title: "Aviso de presupuesto", Body: fmt.Sprintf("Alcanzaste el %d %% de un presupuesto. Revisa tu plan.", a.threshold), PrivateBody: fmt.Sprintf("Alcanzaste el %d %% de un presupuesto: %s gastados. Revisa tu plan.", a.threshold, usd(a.spent)), URL: "/plan"}
		if err := s.queue(ctx, subs, sl, key, "budget", payload, a.at); err != nil {
			return err
		}
	}
	securityText := map[string]string{"login_ok": "Nuevo inicio de sesión en tu cuenta.", "password_changed": "Se cambió tu contraseña.", "totp_enabled": "Se configuró una app de autenticación.", "totp_disabled": "Se quitó tu app de autenticación.", "passkey_added": "Se agregó una passkey.", "passkey_removed": "Se quitó una passkey.", "recovery_regenerated": "Se generaron nuevos códigos de recuperación.", "mfa_failed": "Hubo un intento de acceso con un segundo factor incorrecto.", "login_failed": "Hubo un intento de inicio de sesión fallido.", "session_revoked": "Se cerró una sesión a distancia.", "gmail_connected": "Se conectó una cuenta de Gmail a Pulse.", "gmail_disconnected": "Se desconectó Gmail de Pulse.", "gmail_broken": "Pulse perdió el acceso a tu Gmail. Vuelve a conectarlo en Ajustes.", "gemini_enabled": "Se activó la lectura con Gemini para correos no reconocidos."}
	if p.Security {
		rows, err := s.Store.DB.QueryContext(ctx, `SELECT id,event,created_at FROM audit_log WHERE user_id=? AND created_at>=?`, u.ID, store.TS(s.Store.Clock.Now().Add(-48*time.Hour)))
		if err != nil {
			return err
		}
		type event struct {
			id       int64
			name, ts string
		}
		var events []event
		for rows.Next() {
			var e event
			if err := rows.Scan(&e.id, &e.name, &e.ts); err != nil {
				rows.Close()
				return err
			}
			events = append(events, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, e := range events {
			if body := securityText[e.name]; body != "" {
				eligible := []store.PushSubscription{}
				for _, sub := range subs {
					if e.id > sub.SecurityCursor {
						eligible = append(eligible, sub)
					}
				}
				if err := s.queue(ctx, eligible, sl, fmt.Sprintf("security:%d", e.id), "security", Payload{Title: "Seguridad de Pulse", Body: body, URL: "/ajustes"}, store.ParseTS(e.ts)); err != nil {
					return err
				}
			}
		}
	}
	if now.Hour() < 9 {
		return nil
	}
	if p.Weekly && now.Weekday() == time.Sunday {
		end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if err := s.summary(ctx, u, subs, sl, "weekly", now.Format("2006-01-02"), end.AddDate(0, 0, -7), end); err != nil {
			return err
		}
	}
	if p.Monthly && now.Day() == 1 {
		end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		if err := s.summary(ctx, u, subs, sl, "monthly", now.Format("2006-01"), end.AddDate(0, -1, 0), end); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) summary(ctx context.Context, u store.User, subs []store.PushSubscription, sl *crypto.Sealer, kind, period string, from, to time.Time) error {
	spent, err := s.Store.SpentByCategory(ctx, u.ID, from, to)
	if err != nil {
		return err
	}
	var total int64
	for _, c := range spent {
		total += c
	}
	title := "Resumen semanal"
	if kind == "monthly" {
		title = "Resumen mensual"
	}
	month := budget.MonthOf(from, from.Location())
	return s.queue(ctx, subs, sl, kind+":"+period, kind, Payload{Title: title, Body: "Tu resumen de gastos está listo en Pulse.", PrivateBody: fmt.Sprintf("Gastos del período: %s. Consulta los movimientos en Pulse.", usd(total)), URL: "/movimientos?month=" + month}, time.Time{})
}

func usd(c int64) string {
	if c < 0 {
		return "-$" + usd(-c)[1:]
	}
	return fmt.Sprintf("$%d.%02d", c/100, c%100)
}

func (s *Service) Test(ctx context.Context, uid string) error {
	u, err := s.Store.User(ctx, uid)
	if err != nil {
		return err
	}
	sl, err := s.Keys.Sealer(uid, u.DEKEnc)
	if err != nil {
		return err
	}
	subs, err := s.Store.PushSubscriptions(ctx, uid)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return store.Conflict("Activa las notificaciones en un dispositivo primero.")
	}
	return s.queue(ctx, subs, sl, "test:"+auth.NewID(), "test", Payload{Title: "Pulse", Body: "Las notificaciones están activadas.", URL: "/ajustes"}, time.Time{})
}

// Imported avisa de un gasto que llegó por Gmail. Con "ocultar montos" el texto no dice cuánto ni dónde.
func (s *Service) Imported(ctx context.Context, uid, txID string, amountCents int64, merchant string, confirmed bool) error {
	u, err := s.Store.User(ctx, uid)
	if err != nil {
		return err
	}
	subs, err := s.Store.PushSubscriptions(ctx, uid)
	if err != nil || len(subs) == 0 {
		return err
	}
	sl, err := s.Keys.Sealer(uid, u.DEKEnc)
	if err != nil {
		return err
	}
	title, body := "Nuevo gasto por revisar", "Pulse detectó un consumo en tu correo. Confírmalo en Movimientos."
	if confirmed {
		title, body = "Gasto registrado", "Pulse registró un consumo de tu correo."
	}
	private := fmt.Sprintf("%s en %s.", usd(amountCents), merchant)
	if !confirmed {
		private += " Confírmalo en Movimientos."
	}
	return s.queue(ctx, subs, sl, "import:"+txID, "import", Payload{Title: title, Body: body, PrivateBody: private, URL: "/movimientos"}, time.Time{})
}

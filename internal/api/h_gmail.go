package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pulse/internal/gmail"
	"pulse/internal/importer"
	"pulse/internal/mailparse"
	"pulse/internal/store"
)

var errGmailOff = errf(503, "gmail_disabled", "La conexión con Gmail todavía no está configurada en el servidor.")

type gmailStatus struct {
	Available       bool       `json:"available"`
	GeminiAvailable bool       `json:"gemini_available"`
	Realtime        bool       `json:"realtime"`
	PollMinutes     int        `json:"poll_minutes"`
	Senders         []string   `json:"senders"`
	Connected       bool       `json:"connected"`
	Email           string     `json:"email,omitempty"`
	Status          string     `json:"status,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LastOKAt        *time.Time `json:"last_ok_at,omitempty"`
	NextSyncAt      *time.Time `json:"next_sync_at,omitempty"`
	WatchExpiresAt  *time.Time `json:"watch_expires_at,omitempty"`
	Shadow          bool       `json:"shadow"`
	Gemini          bool       `json:"gemini"`
	Imported        int        `json:"imported"`
	// Precisión por banco en tus revisiones y correos que ningún parser entendió (últimos 30 días).
	Accuracy       []bankAccuracy `json:"accuracy"`
	Templates      []bankTemplate `json:"template_alerts"`
	TemplatesTotal int            `json:"template_alerts_total"`
}

type bankAccuracy struct {
	store.ParserAccuracy
	Bank    string `json:"bank"`
	Trusted bool   `json:"trusted"`
}

type bankTemplate struct {
	store.TemplateAlert
	Bank string `json:"bank"`
}

// shadowReport arma las métricas del modo sombra y el detector de plantillas.
func (s *Server) shadowReport(r *http.Request, id *identity, out *gmailStatus) error {
	acc, err := s.store.ParserAccuracy(r.Context(), id.user.ID)
	if err != nil {
		return err
	}
	out.Accuracy = []bankAccuracy{}
	for _, a := range acc {
		out.Accuracy = append(out.Accuracy, bankAccuracy{a, mailparse.Bank(a.Parser), a.Trusted()})
	}
	alerts, err := s.store.TemplateAlerts(r.Context(), id.user.ID, id.seal(), s.clock.Now().AddDate(0, 0, -30))
	if err != nil {
		return err
	}
	out.Templates = []bankTemplate{}
	for _, a := range alerts {
		// Earlier versions also raised alarms for offers and card payments.
		// Filter before limiting the preview so they cannot hide real failures.
		if _, ignored := mailparse.IgnoredSubject(a.Subject); ignored {
			continue
		}
		out.TemplatesTotal++
		if len(out.Templates) < 20 {
			out.Templates = append(out.Templates, bankTemplate{a, mailparse.Bank(a.Parser)})
		}
	}
	return nil
}

func (s *Server) gmailStatus(w http.ResponseWriter, r *http.Request) error {
	g := s.gm()
	out := gmailStatus{Available: g != nil, Senders: mailparse.Senders(), Shadow: true}
	if err := s.shadowReport(r, ident(r), &out); err != nil {
		return err
	}
	if g == nil {
		writeJSON(w, 200, out)
		return nil
	}
	out.GeminiAvailable = g.Gemini != nil
	out.Realtime = g.Topic != ""
	out.PollMinutes = int(g.Interval / time.Minute)
	id := ident(r)
	c, err := s.store.Gmail(r.Context(), id.user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 200, out)
		return nil
	}
	if err != nil {
		return err
	}
	email, err := id.seal().Open("gmail.email", c.EmailEnc)
	if err != nil {
		return err
	}
	out.Connected, out.Email, out.Status, out.LastError = true, email, c.Status, c.LastError
	out.LastSyncAt, out.LastOKAt, out.WatchExpiresAt = c.LastSyncAt, c.LastOKAt, c.WatchExpiresAt
	if c.Status == "active" {
		next := c.NextSyncAt
		out.NextSyncAt = &next
	}
	out.Shadow, out.Gemini, out.Imported = c.Shadow, c.Gemini, c.Imported
	writeJSON(w, 200, out)
	return nil
}

// POST /api/gmail/connect (con step-up): devuelve la URL de Google a la que ir.
func (s *Server) gmailConnect(w http.ResponseWriter, r *http.Request) error {
	g := s.gm()
	if g == nil {
		return errGmailOff
	}
	id := ident(r)
	state, challenge := gmail.NewState(s.keys.Lookup, id.user.ID, id.sess.ID, s.clock.Now())
	writeJSON(w, 200, map[string]string{"url": g.Client.AuthURL(state, challenge, "")})
	return nil
}

// POST /api/gmail/callback: la página /ajustes/gmail reenvía aquí el código que dio Google.
// (Google vuelve con una navegación entre sitios: la cookie SameSite=Strict no viajaría a
// un GET directo; por eso el canje lo hace el frontend con fetch desde el propio origen.)
func (s *Server) gmailCallback(w http.ResponseWriter, r *http.Request) error {
	g := s.gm()
	if g == nil {
		return errGmailOff
	}
	id := ident(r)
	if !s.limGmail.Allow(id.user.ID) {
		return errRateLimited
	}
	var in struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	verifier, err := gmail.CheckState(s.keys.Lookup, in.State, id.user.ID, id.sess.ID, s.clock.Now())
	if err != nil {
		s.audit(r, id.user.ID, "gmail_connect_failed", map[string]string{"reason": "state"})
		return badRequest("El enlace de Google caducó o no es de esta sesión. Vuelve a conectar.")
	}
	if in.Code == "" || len(in.Code) > 512 {
		return badRequest("Falta el código de Google.")
	}
	email, err := g.Connect(r.Context(), id.user, id.seal(), in.Code, verifier)
	if err != nil {
		var ae *gmail.APIError
		if errors.As(err, &ae) {
			s.audit(r, id.user.ID, "gmail_connect_failed", map[string]string{"reason": ae.Code})
			return errf(502, "google", "Google no aceptó la conexión. Vuelve a intentarlo.")
		}
		return err
	}
	s.audit(r, id.user.ID, "gmail_connected", nil)
	writeJSON(w, 200, map[string]string{"email": email})
	return nil
}

// POST /api/gmail/sync: "Sincronizar ahora".
func (s *Server) gmailSync(w http.ResponseWriter, r *http.Request) error {
	g := s.gm()
	if g == nil {
		return errGmailOff
	}
	id := ident(r)
	if !s.limGmail.Allow(id.user.ID) {
		return errRateLimited
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var res *gmail.Result
	var err error
	if r.URL.Query().Get("retry_unrecognized") == "true" {
		res, err = g.RetryUnrecognized(ctx, id.user.ID)
	} else {
		res, err = g.SyncUser(ctx, id.user.ID)
	}
	if err != nil {
		if gmail.Temporary(err) && !errors.Is(err, context.DeadlineExceeded) {
			return errf(502, "google", "Gmail no respondió. Pulse lo reintentará solo en unos minutos.")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return errf(504, "timeout", "La lectura tardó demasiado. Puedes reintentar; los gastos ya registrados se conservan.")
		}
		return err
	}
	writeJSON(w, 200, res)
	return nil
}

func (s *Server) gmailOptions(w http.ResponseWriter, r *http.Request) error {
	g := s.gm()
	if g == nil {
		return errGmailOff
	}
	var in struct {
		Shadow *bool `json:"shadow"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	id := ident(r)
	if err := s.store.SetGmailOptions(r.Context(), id.user.ID, in.Shadow, nil); err != nil {
		return err
	}
	if in.Shadow != nil {
		s.audit(r, id.user.ID, "gmail_shadow_changed", map[string]string{"shadow": strconv.FormatBool(*in.Shadow)})
	}
	w.WriteHeader(204)
	return nil
}

// PUT /api/gmail/gemini (con step-up): enviar correos redactados a un tercero es una
// decisión de privacidad, así que exige confirmar identidad y queda auditada.
func (s *Server) gmailGemini(w http.ResponseWriter, r *http.Request) error {
	g := s.gm()
	if g == nil {
		return errGmailOff
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Enabled && g.Gemini == nil {
		return errf(409, "gemini_disabled", "El servidor no tiene Gemini configurado.")
	}
	id := ident(r)
	if err := s.store.SetGmailOptions(r.Context(), id.user.ID, nil, &in.Enabled); err != nil {
		return err
	}
	ev := "gemini_disabled"
	if in.Enabled {
		ev = "gemini_enabled"
	}
	s.audit(r, id.user.ID, ev, nil)
	w.WriteHeader(204)
	return nil
}

func (s *Server) gmailDisconnect(w http.ResponseWriter, r *http.Request) error {
	g := s.gm()
	if g == nil {
		return errGmailOff
	}
	id := ident(r)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := g.Disconnect(ctx, id.user, id.seal()); err != nil {
		return err
	}
	s.audit(r, id.user.ID, "gmail_disconnected", nil)
	w.WriteHeader(204)
	return nil
}

// POST /api/gmail/webhook: aviso push de Pub/Sub. No usa cookies: lo autentica el JWT de Google.
// Siempre responde rápido (Pub/Sub reintenta si tarda); la sincronización corre aparte.
func (s *Server) gmailWebhook(w http.ResponseWriter, r *http.Request) {
	rt := s.gmailRT.Load()
	if rt == nil || rt.pubsub == nil {
		w.WriteHeader(404)
		return
	}
	g := rt.syncer
	if !s.limWebhook.Allow(clientIP(r)) {
		w.WriteHeader(429)
		return
	}
	if err := rt.pubsub.Verify(r.Context(), r.Header.Get("Authorization")); err != nil {
		s.log.Warn("gmail: aviso de Pub/Sub rechazado", "ip", clientIP(r))
		w.WriteHeader(401)
		return
	}
	var env struct {
		Message struct {
			Data string `json:"data"`
		} `json:"message"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil || json.Unmarshal(body, &env) != nil {
		w.WriteHeader(400)
		return
	}
	raw, err := base64.StdEncoding.DecodeString(env.Message.Data)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(env.Message.Data)
	}
	var note struct {
		EmailAddress string          `json:"emailAddress"`
		HistoryID    json.RawMessage `json:"historyId"`
	}
	if err != nil || json.Unmarshal(raw, &note) != nil || !strings.Contains(note.EmailAddress, "@") {
		w.WriteHeader(204) // mensaje malformado: no tiene sentido que Pub/Sub lo reintente
		return
	}
	hid, _ := strconv.ParseUint(strings.Trim(string(note.HistoryID), `"`), 10, 64)
	uid, err := s.store.GmailUserByEmailHash(r.Context(), g.EmailHash(note.EmailAddress))
	if err == nil {
		if fresh, err := s.store.GmailTrigger(r.Context(), uid, hid); err == nil && fresh {
			g.Trigger(uid)
		}
	}
	w.WriteHeader(204) // igual para cuentas desconocidas: no revela quién usa Pulse
}

// AfterImport se llama por cada gasto que llega por Gmail: recalcula presupuestos (que
// dispara las alertas push) y avisa del gasto nuevo si es reciente.
func (s *Server) AfterImport(ctx context.Context, u *store.User, out *importer.Outcome) {
	t := out.Transaction
	if t == nil {
		return
	}
	if err := s.RecheckBudgets(ctx, u, []string{t.CategoryID}, []time.Time{t.OccurredAt}); err != nil {
		s.log.Error("recalculando presupuestos", "err", err.Error())
	}
	if s.push != nil && s.clock.Now().Sub(t.OccurredAt) < 24*time.Hour {
		if err := s.push.Imported(ctx, u.ID, t.ID, t.AmountCents, t.Merchant, t.Status == "confirmed"); err != nil {
			s.log.Error("avisando gasto importado", "err", err.Error())
		}
	}
}

// GmailBroken registra en la auditoría que la conexión se rompió (el push lo avisa como seguridad).
func (s *Server) GmailBroken(ctx context.Context, userID, code string) {
	s.store.Audit(ctx, userID, "gmail_broken", "", "pulse", map[string]string{"reason": code})
}

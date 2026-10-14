package gmail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"pulse/internal/clock"
	"pulse/internal/crypto"
	"pulse/internal/importer"
	"pulse/internal/mailparse"
	"pulse/internal/store"
)

const (
	maxPages         = 5 // 500 mensajes por sincronización: de sobra para notificaciones bancarias
	firstSyncDays    = 7
	maxBackoff       = 6 * time.Hour
	watchRenewBefore = 24 * time.Hour
)

var (
	ErrNotConnected = store.Conflict("Gmail no está conectado.")
	ErrBroken       = store.Conflict("La conexión con Gmail se perdió. Vuelve a conectarla.")
)

// Result resume una sincronización (se muestra al pulsar "Sincronizar ahora").
type Result struct {
	Found        int `json:"found"`
	Created      int `json:"created"`
	Confirmed    int `json:"confirmed"`
	Duplicate    int `json:"duplicate"`
	Ignored      int `json:"ignored"`
	Unrecognized int `json:"unrecognized"`
	Gemini       int `json:"gemini"`
}

type Syncer struct {
	Store    *store.Store
	Keys     *crypto.Keyring
	Client   *Client
	Importer *importer.Importer
	Gemini   *Gemini // nil: el servidor no tiene clave de Gemini
	Clock    clock.Clock
	Log      *slog.Logger
	Interval time.Duration
	Topic    string // vacío: sin Pub/Sub, solo polling

	// OnImported se llama por cada gasto nuevo (recalcular presupuestos, avisar por push).
	OnImported func(ctx context.Context, u *store.User, out *importer.Outcome)
	// OnBroken se llama cuando la conexión queda rota (auditoría → aviso de seguridad).
	OnBroken func(ctx context.Context, userID, code string)

	once    sync.Once
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
	tokens  map[string]cachedToken
	trigger chan string
}

type cachedToken struct {
	access string
	exp    time.Time
}

func (s *Syncer) init() {
	s.once.Do(func() {
		s.locks = map[string]*sync.Mutex{}
		s.tokens = map[string]cachedToken{}
		s.trigger = make(chan string, 64)
	})
}

// lock serializa las sincronizaciones de un mismo usuario (polling, Pub/Sub y botón a la vez).
func (s *Syncer) lock(uid string) func() {
	s.init()
	s.mu.Lock()
	l, ok := s.locks[uid]
	if !ok {
		l = &sync.Mutex{}
		s.locks[uid] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// Trigger pide una sincronización pronto, sin bloquear (aviso de Pub/Sub).
func (s *Syncer) Trigger(uid string) {
	s.init()
	select {
	case s.trigger <- uid:
	default: // cola llena: el polling lo recogerá igual
	}
}

// Run sincroniza a quien le toque y atiende los avisos de Pub/Sub hasta que ctx termine.
func (s *Syncer) Run(ctx context.Context) {
	s.init()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	prune := time.NewTicker(6 * time.Hour)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case uid := <-s.trigger:
			s.syncLogged(ctx, uid)
		case <-tick.C:
			ids, err := s.Store.DueGmail(ctx)
			if err != nil {
				s.log().Error("gmail: listando sincronizaciones", "err", err.Error())
				continue
			}
			for _, uid := range ids {
				if ctx.Err() != nil {
					return
				}
				s.syncLogged(ctx, uid)
			}
		case <-prune.C:
			if err := s.Store.PruneGmailSeen(ctx); err != nil {
				s.log().Error("gmail: limpiando mensajes vistos", "err", err.Error())
			}
		}
	}
}

func (s *Syncer) syncLogged(ctx context.Context, uid string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	r, err := s.SyncUser(ctx, uid)
	if err != nil {
		if !errors.Is(err, ErrBroken) && !errors.Is(err, ErrNotConnected) {
			s.log().Warn("gmail: sincronización fallida", "user", uid, "err", err.Error())
		}
		return
	}
	// Solo cifras: nunca comercios, montos ni direcciones en el log.
	s.log().Info("gmail: sincronizado", "user", uid, "found", r.Found, "created", r.Created)
}

func (s *Syncer) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

// SyncUser trae los correos bancarios nuevos de un usuario y los importa.
func (s *Syncer) SyncUser(ctx context.Context, uid string) (*Result, error) {
	defer s.lock(uid)()
	conn, err := s.Store.Gmail(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotConnected
	}
	if err != nil {
		return nil, err
	}
	if conn.Status != "active" {
		return nil, ErrBroken
	}
	u, err := s.Store.User(ctx, uid)
	if err != nil {
		return nil, err
	}
	if u.DisabledAt != nil {
		return nil, ErrNotConnected
	}
	sl, err := s.Keys.Sealer(u.ID, u.DEKEnc)
	if err != nil {
		return nil, err
	}
	var res *Result
	for attempt := 0; ; attempt++ {
		var access string
		access, err = s.access(ctx, u.ID, conn, sl)
		if err == nil {
			res, err = s.fetch(ctx, u, conn, access)
		}
		var ae *APIError
		if attempt == 0 && errors.As(err, &ae) && ae.Status == 401 {
			s.forget(u.ID) // el access token caducó antes de lo previsto: uno nuevo y otra vez
			continue
		}
		break
	}
	if err != nil {
		return res, s.fail(ctx, u.ID, conn, err)
	}
	if s.Topic != "" {
		s.renewWatch(ctx, u.ID, conn, sl)
	}
	return res, s.Store.GmailSynced(ctx, u.ID, s.Clock.Now().Add(s.Interval), res.Created)
}

func (s *Syncer) fail(ctx context.Context, uid string, conn *store.GmailConnection, err error) error {
	var ae *APIError
	code := "red"
	switch {
	case errors.Is(err, ErrRevoked):
		code = "revocado"
	case errors.As(err, &ae) && ae.Status == 401:
		code = "sin_autorizacion"
	case errors.As(err, &ae) && ae.Status == 429:
		code = "limite_de_google"
	case errors.As(err, &ae) && ae.Status >= 500:
		code = "google_no_disponible"
	case errors.As(err, &ae):
		code = fmt.Sprintf("google_%d", ae.Status)
	case ctx.Err() != nil:
		code = "tiempo_agotado"
	}
	if code == "revocado" || code == "sin_autorizacion" {
		s.forget(uid)
		if e := s.Store.GmailBroken(context.WithoutCancel(ctx), uid, code); e != nil {
			return e
		}
		if s.OnBroken != nil {
			s.OnBroken(context.WithoutCancel(ctx), uid, code)
		}
		return ErrBroken
	}
	// Temporal: espera exponencial (1 min, 2, 4… hasta 6 h) o lo que pida Google.
	delay := time.Minute << min(conn.Failures, 9)
	if errors.As(err, &ae) && ae.RetryAfter > delay {
		delay = ae.RetryAfter
	}
	delay = min(delay, maxBackoff)
	if e := s.Store.GmailFailed(context.WithoutCancel(ctx), uid, code, s.Clock.Now().Add(delay)); e != nil {
		return e
	}
	return err
}

func (s *Syncer) access(ctx context.Context, uid string, conn *store.GmailConnection, sl *crypto.Sealer) (string, error) {
	s.init()
	s.mu.Lock()
	t, ok := s.tokens[uid]
	s.mu.Unlock()
	if ok && s.Clock.Now().Add(time.Minute).Before(t.exp) {
		return t.access, nil
	}
	refresh, err := sl.Open("gmail.refresh", conn.RefreshEnc)
	if err != nil {
		return "", err
	}
	tok, err := s.Client.Refresh(ctx, refresh)
	if err != nil {
		return "", err
	}
	s.remember(uid, tok)
	return tok.AccessToken, nil
}

func (s *Syncer) remember(uid string, tok *Token) {
	s.init()
	exp := time.Duration(tok.ExpiresIn) * time.Second
	if exp <= 0 || exp > time.Hour {
		exp = 30 * time.Minute
	}
	s.mu.Lock()
	s.tokens[uid] = cachedToken{tok.AccessToken, s.Clock.Now().Add(exp)}
	s.mu.Unlock()
}

func (s *Syncer) forget(uid string) {
	s.init()
	s.mu.Lock()
	delete(s.tokens, uid)
	s.mu.Unlock()
}

// Query devuelve la búsqueda de Gmail: solo remitentes bancarios y solo días recientes.
func Query(days int) string {
	return "from:(" + strings.Join(mailparse.Senders(), " OR ") + fmt.Sprintf(") newer_than:%dd", days)
}

func (s *Syncer) days(conn *store.GmailConnection) int {
	if conn.LastOKAt == nil {
		return firstSyncDays
	}
	d := int(s.Clock.Now().Sub(*conn.LastOKAt).Hours()/24) + 2 // margen: correos que llegan tarde
	return min(max(d, 2), 30)
}

func (s *Syncer) fetch(ctx context.Context, u *store.User, conn *store.GmailConnection, access string) (*Result, error) {
	res := &Result{}
	var ids []string
	page := ""
	for i := 0; i < maxPages; i++ {
		got, next, err := s.Client.List(ctx, access, Query(s.days(conn)), page)
		if err != nil {
			return res, err
		}
		ids = append(ids, got...)
		if next == "" {
			break
		}
		page = next
	}
	res.Found = len(ids)
	for i := len(ids) - 1; i >= 0; i-- { // del más viejo al más nuevo
		if err := s.process(ctx, u, conn, access, ids[i], res); err != nil {
			return res, err
		}
	}
	return res, nil
}

func (s *Syncer) process(ctx context.Context, u *store.User, conn *store.GmailConnection, access, id string, res *Result) error {
	h := s.Keys.Lookup("gmail-msg", []byte(u.ID+"|"+id))
	claimed, err := s.Store.ClaimGmailMessage(ctx, u.ID, h)
	if err != nil {
		return err
	}
	if !claimed {
		res.Duplicate++
		return nil
	}
	release := func(err error) error {
		s.Store.ReleaseGmailMessage(context.WithoutCancel(ctx), u.ID, h)
		return err
	}
	raw, err := s.Client.Raw(ctx, access, id)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 404 { // borrado entre la lista y la descarga
			return nil
		}
		return release(err)
	}
	e, err := mailparse.ParseEML(bytes.NewReader(raw))
	clear(raw) // el correo no se conserva
	if err != nil {
		res.Unrecognized++
		return nil
	}
	out, err := s.Importer.Import(ctx, u, e, "gmail")
	if err != nil {
		return release(err)
	}
	if out.Status == mailparse.StatusUnrecognized && conn.Gemini && s.Gemini != nil {
		out, err = s.tryGemini(ctx, u, e, out)
		if err != nil {
			return release(err)
		}
	}
	switch out.Status {
	case "created":
		res.Created++
		if out.Parser == "gemini" {
			res.Gemini++
		}
		// Modo sombra apagado: lo que el parser lee con seguridad y tiene categoría conocida
		// se confirma solo. Lo dudoso (Gemini, sin tarjeta, categoría por defecto) sigue por revisar.
		if !conn.Shadow && out.Transaction.Confidence != nil && *out.Transaction.Confidence >= 0.9 && out.CategoryBy == "rule" {
			if err := s.Store.ConfirmImported(ctx, u.ID, out.Transaction.ID); err != nil {
				return err
			}
			out.Transaction.Status = "confirmed"
			res.Confirmed++
		}
		if s.OnImported != nil {
			s.OnImported(ctx, u, out)
		}
	case "duplicate":
		res.Duplicate++
	case mailparse.StatusUnrecognized:
		res.Unrecognized++
	default:
		res.Ignored++
	}
	return nil
}

func (s *Syncer) tryGemini(ctx context.Context, u *store.User, e *mailparse.Email, prev *importer.Outcome) (*importer.Outcome, error) {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		loc = time.UTC
	}
	x, why, err := s.Gemini.Extract(ctx, e, loc)
	if err != nil {
		// Gemini caído no detiene la sincronización: el correo queda como no reconocido.
		s.log().Warn("gmail: gemini no disponible", "err", err.Error())
		return prev, nil
	}
	if x == nil {
		return &importer.Outcome{Status: mailparse.StatusUnrecognized, Reason: why, Parser: "gemini"}, nil
	}
	return s.Importer.ImportExpense(ctx, u, e, x, "gmail")
}

func (s *Syncer) renewWatch(ctx context.Context, uid string, conn *store.GmailConnection, sl *crypto.Sealer) {
	if conn.WatchExpiresAt != nil && conn.WatchExpiresAt.After(s.Clock.Now().Add(watchRenewBefore)) {
		return
	}
	access, err := s.access(ctx, uid, conn, sl)
	if err != nil {
		return
	}
	h, exp, err := s.Client.Watch(ctx, access, s.Topic)
	if err != nil {
		s.log().Warn("gmail: no se pudo renovar watch", "user", uid, "err", err.Error())
		return
	}
	if err := s.Store.GmailWatched(ctx, uid, h, exp); err != nil {
		s.log().Error("gmail: guardando watch", "err", err.Error())
	}
}

// Connect completa el OAuth: canjea el código, comprueba el permiso y guarda el token cifrado.
func (s *Syncer) Connect(ctx context.Context, u *store.User, sl *crypto.Sealer, code, verifier string) (string, error) {
	tok, err := s.Client.Exchange(ctx, code, verifier)
	if err != nil {
		return "", err
	}
	if !strings.Contains(" "+tok.Scope+" ", " "+Scope+" ") {
		s.Client.Revoke(ctx, tok.AccessToken)
		return "", store.Conflict("Pulse necesita permiso para leer tus correos. Vuelve a conectar y marca la casilla de Gmail.")
	}
	if tok.RefreshToken == "" {
		s.Client.Revoke(ctx, tok.AccessToken)
		return "", store.Conflict("Google no entregó acceso permanente. Vuelve a intentarlo.")
	}
	p, err := s.Client.Profile(ctx, tok.AccessToken)
	if err != nil {
		return "", err
	}
	email := strings.ToLower(strings.TrimSpace(p.EmailAddress))
	err = s.Store.SaveGmail(ctx, u.ID, sl.Seal("gmail.email", email), s.Keys.Lookup("gmail-email", []byte(email)), sl.Seal("gmail.refresh", tok.RefreshToken))
	if err != nil {
		s.Client.Revoke(ctx, tok.RefreshToken)
		return "", err
	}
	s.remember(u.ID, tok)
	s.Trigger(u.ID)
	return email, nil
}

// Disconnect detiene el watch, revoca el token en Google y borra la conexión.
// Aunque Google no responda, la conexión se borra igual (el token queda inservible sin la BD).
func (s *Syncer) Disconnect(ctx context.Context, u *store.User, sl *crypto.Sealer) error {
	defer s.lock(u.ID)()
	conn, err := s.Store.Gmail(ctx, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotConnected
	}
	if err != nil {
		return err
	}
	if refresh, err := sl.Open("gmail.refresh", conn.RefreshEnc); err == nil {
		if s.Topic != "" && conn.Status == "active" {
			if access, err := s.access(ctx, u.ID, conn, sl); err == nil {
				s.Client.Stop(ctx, access)
			}
		}
		if err := s.Client.Revoke(ctx, refresh); err != nil {
			s.log().Warn("gmail: no se pudo revocar en Google", "err", err.Error())
		}
	}
	s.forget(u.ID)
	return s.Store.DeleteGmail(ctx, u.ID)
}

// EmailHash es la clave con la que se encuentra al usuario desde un aviso de Pub/Sub.
func (s *Syncer) EmailHash(email string) []byte {
	return s.Keys.Lookup("gmail-email", []byte(strings.ToLower(strings.TrimSpace(email))))
}

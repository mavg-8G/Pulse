package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pulse/internal/config"
	"pulse/internal/gmail"
	"pulse/internal/store"
)

// GoogleSettings son las credenciales de Google que activan Gmail. Vienen de las variables
// de entorno o, si un admin las guardó en Ajustes, de la base (cifradas con la KEK).
type GoogleSettings struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	PubSubTopic  string `json:"pubsub_topic,omitempty"`
	PubSubSA     string `json:"pubsub_sa,omitempty"`
	GeminiKey    string `json:"gemini_key,omitempty"`
}

const googleSettingKey = "google"

// gmailRuntime es la integración activa; se reemplaza entera al cambiar las credenciales.
type gmailRuntime struct {
	syncer *gmail.Syncer
	pubsub *gmail.PubSubVerifier
	source string // "env" | "ajustes"
	cfg    GoogleSettings
}

func (s *Server) gm() *gmail.Syncer {
	if rt := s.gmailRT.Load(); rt != nil {
		return rt.syncer
	}
	return nil
}

func (s *Server) buildGmail(gs GoogleSettings, source string) *gmailRuntime {
	httpc := gmail.NewHTTPClient()
	ep := s.googleEP
	syn := &gmail.Syncer{Store: s.store, Keys: s.keys, Clock: s.clock, Log: s.log,
		Interval: s.cfg.GmailPollInterval, Topic: gs.PubSubTopic, Importer: s.importer(),
		Client:     &gmail.Client{ClientID: gs.ClientID, ClientSecret: gs.ClientSecret, RedirectURL: s.cfg.Origin() + "/ajustes/gmail", EP: ep, HTTP: httpc},
		OnImported: s.AfterImport, OnBroken: s.GmailBroken}
	if gs.GeminiKey != "" {
		syn.Gemini = &gmail.Gemini{Key: gs.GeminiKey, Model: s.cfg.GeminiModel, Base: ep.Gemini, HTTP: httpc}
	}
	rt := &gmailRuntime{syncer: syn, source: source, cfg: gs}
	if gs.PubSubTopic != "" {
		rt.pubsub = &gmail.PubSubVerifier{CertsURL: ep.Certs, Audience: s.cfg.GmailPubSubAudience, ServiceAccount: gs.PubSubSA, HTTP: httpc, Now: s.clock.Now}
	}
	return rt
}

// loadGoogle devuelve las credenciales guardadas en Ajustes, o nil si no hay.
func (s *Server) loadGoogle(ctx context.Context) (*GoogleSettings, error) {
	blob, err := s.store.Setting(ctx, googleSettingKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw, err := s.keys.Server().OpenBytes(googleSettingKey, blob)
	if err != nil {
		return nil, err
	}
	var gs GoogleSettings
	return &gs, json.Unmarshal(raw, &gs)
}

// setGmail activa rt (nil = sin Gmail): detiene la sincronización anterior y, si el
// servidor ya está corriendo (RunGmail), arranca la nueva.
func (s *Server) setGmail(rt *gmailRuntime) {
	s.gmailMu.Lock()
	defer s.gmailMu.Unlock()
	if s.gmailStop != nil {
		s.gmailStop()
		s.gmailStop = nil
	}
	s.gmailRT.Store(rt)
	if rt == nil || s.gmailCtx == nil {
		return
	}
	ctx, cancel := context.WithCancel(s.gmailCtx)
	done := make(chan struct{})
	go func() { defer close(done); rt.syncer.Run(ctx) }()
	s.gmailStop = func() { cancel(); <-done }
}

// RunGmail sincroniza Gmail en segundo plano hasta que ctx termine, aplicando en caliente
// los cambios de credenciales que haga un admin.
func (s *Server) RunGmail(ctx context.Context) {
	s.gmailMu.Lock()
	s.gmailCtx = ctx
	s.gmailMu.Unlock()
	s.setGmail(s.gmailRT.Load())
	<-ctx.Done()
	s.setGmail(nil)
}

type googleAdminView struct {
	Source          string `json:"source"` // "" (sin configurar) | "env" | "ajustes"
	ClientID        string `json:"client_id"`
	ClientSecretSet bool   `json:"client_secret_set"`
	PubSubTopic     string `json:"pubsub_topic"`
	PubSubSA        string `json:"pubsub_sa"`
	GeminiKeySet    bool   `json:"gemini_key_set"`
	RedirectURI     string `json:"redirect_uri"`
	WebhookURL      string `json:"webhook_url"`
	Audience        string `json:"audience"`
}

func (s *Server) adminOnly(fn handler) http.HandlerFunc {
	return s.withStepUp(func(w http.ResponseWriter, r *http.Request) error {
		if ident(r).user.Role != "admin" {
			return errf(403, "forbidden", "Solo un administrador puede cambiar esto.")
		}
		return fn(w, r)
	})
}

// GET /api/admin/gmail: lo necesario para configurar Google Cloud, sin devolver secretos.
func (s *Server) adminGmail(w http.ResponseWriter, r *http.Request) error {
	if ident(r).user.Role != "admin" {
		return errf(403, "forbidden", "Solo un administrador puede ver esto.")
	}
	out := googleAdminView{RedirectURI: s.cfg.Origin() + "/ajustes/gmail", WebhookURL: s.cfg.Origin() + "/api/gmail/webhook", Audience: s.cfg.GmailPubSubAudience}
	if rt := s.gmailRT.Load(); rt != nil {
		out.Source, out.ClientID, out.ClientSecretSet = rt.source, rt.cfg.ClientID, rt.cfg.ClientSecret != ""
		out.PubSubTopic, out.PubSubSA, out.GeminiKeySet = rt.cfg.PubSubTopic, rt.cfg.PubSubSA, rt.cfg.GeminiKey != ""
	}
	writeJSON(w, 200, out)
	return nil
}

// PUT /api/admin/gmail (admin + step-up): guarda las credenciales y las aplica sin reiniciar.
// Un secreto vacío conserva el actual; "gemini_key": "-" lo borra.
func (s *Server) adminSetGmail(w http.ResponseWriter, r *http.Request) error {
	var in GoogleSettings
	if err := decode(w, r, &in); err != nil {
		return err
	}
	in.ClientID, in.ClientSecret = strings.TrimSpace(in.ClientID), strings.TrimSpace(in.ClientSecret)
	in.PubSubTopic, in.PubSubSA, in.GeminiKey = strings.TrimSpace(in.PubSubTopic), strings.TrimSpace(in.PubSubSA), strings.TrimSpace(in.GeminiKey)
	prev := GoogleSettings{}
	if rt := s.gmailRT.Load(); rt != nil {
		prev = rt.cfg
	}
	if in.ClientSecret == "" && in.ClientID == prev.ClientID {
		in.ClientSecret = prev.ClientSecret // el mismo cliente: no hace falta volver a pegar el secreto
	}
	switch in.GeminiKey {
	case "":
		in.GeminiKey = prev.GeminiKey
	case "-":
		in.GeminiKey = ""
	}
	switch {
	case !config.ValidGoogleClientID(in.ClientID):
		return badRequest("El ID de cliente debe terminar en .apps.googleusercontent.com.")
	case in.ClientSecret == "" || len(in.ClientSecret) > 200:
		return badRequest("Falta el secreto del cliente de OAuth.")
	case in.PubSubTopic != "" && !config.ValidPubSubTopic(in.PubSubTopic):
		return badRequest("El tema de Pub/Sub debe ser projects/<proyecto>/topics/<tema>.")
	case in.PubSubTopic != "" && !config.ValidServiceAccount(in.PubSubSA):
		return badRequest("Con Pub/Sub, indica la cuenta de servicio que firma los avisos (…@….iam.gserviceaccount.com).")
	case len(in.GeminiKey) > 200:
		return badRequest("La clave de Gemini no es válida.")
	}
	if in.PubSubTopic == "" {
		in.PubSubSA = ""
	}
	raw, _ := json.Marshal(in)
	if err := s.store.SetSetting(r.Context(), googleSettingKey, s.keys.Server().SealBytes(googleSettingKey, raw)); err != nil {
		return err
	}
	s.setGmail(s.buildGmail(in, "ajustes"))
	s.audit(r, ident(r).user.ID, "gmail_server_configured", map[string]string{"realtime": strconv.FormatBool(in.PubSubTopic != ""), "gemini": strconv.FormatBool(in.GeminiKey != "")})
	return s.adminGmail(w, r)
}

// DELETE /api/admin/gmail (admin + step-up): borra lo guardado en Ajustes y vuelve a la
// configuración por variables de entorno, si la hay.
func (s *Server) adminDeleteGmail(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteSetting(r.Context(), googleSettingKey); err != nil {
		return err
	}
	var rt *gmailRuntime
	if s.googleEnv != nil {
		rt = s.buildGmail(*s.googleEnv, "env")
	}
	s.setGmail(rt)
	s.audit(r, ident(r).user.ID, "gmail_server_cleared", nil)
	return s.adminGmail(w, r)
}

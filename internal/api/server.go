// Package api expone la API HTTP de Pulse y sirve el frontend embebido.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"pulse/internal/auth"
	"pulse/internal/buildinfo"
	"pulse/internal/clock"
	"pulse/internal/config"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/gmail"
	"pulse/internal/importer"
	"pulse/internal/passkey"
	"pulse/internal/push"
	"pulse/internal/store"
)

type Deps struct {
	Config *config.Config
	Store  *store.Store
	Keys   *crypto.Keyring
	Hasher *auth.Hasher
	Clock  clock.Clock
	Log    *slog.Logger
	Static http.Handler // frontend; nil en tests de API
	Push   *push.Service
	// Google: credenciales por variables de entorno (nil: ninguna). Las guardadas en
	// Ajustes por un admin tienen prioridad.
	Google   *GoogleSettings
	GoogleEP *gmail.Endpoints // nil: los de Google; el servidor falso en pruebas
}

type Server struct {
	cfg     *config.Config
	store   *store.Store
	keys    *crypto.Keyring
	hasher  *auth.Hasher
	clock   clock.Clock
	log     *slog.Logger
	rp      passkey.RP
	secure  bool
	static  http.Handler
	push    *push.Service
	limPush *auth.Limiter

	limLogin    *auth.Limiter
	limRegister *auth.Limiter
	limImport   *auth.Limiter
	limGmail    *auth.Limiter
	limWebhook  *auth.Limiter
	limMFA      *auth.Limiter

	googleEnv *GoogleSettings
	googleEP  gmail.Endpoints
	gmailRT   atomic.Pointer[gmailRuntime]
	gmailMu   sync.Mutex // serializa setGmail
	gmailCtx  context.Context
	gmailStop func()
}

func New(d Deps) *Server {
	s := &Server{
		cfg: d.Config, store: d.Store, keys: d.Keys, hasher: d.Hasher, clock: d.Clock, log: d.Log, static: d.Static,
		push:        d.Push,
		limPush:     auth.NewLimiter(d.Clock, 1, 1, time.Minute),
		rp:          passkey.RP{ID: d.Config.RPID(), Origin: d.Config.Origin(), Name: "Pulse"},
		secure:      d.Config.BaseURL.Scheme == "https",
		limLogin:    auth.NewLimiter(d.Clock, 10, 10, 15*time.Minute),
		limRegister: auth.NewLimiter(d.Clock, d.Config.RegisterPerHour, d.Config.RegisterPerHour, time.Hour),
		limImport:   auth.NewLimiter(d.Clock, 30, 30, time.Hour),
		limGmail:    auth.NewLimiter(d.Clock, 3, 3, time.Minute),
		limWebhook:  auth.NewLimiter(d.Clock, 120, 120, time.Minute),
		limMFA:      auth.NewLimiter(d.Clock, 6, 6, 5*time.Minute),
		googleEnv:   d.Google,
		googleEP:    gmail.GoogleEndpoints(),
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if d.GoogleEP != nil {
		s.googleEP = *d.GoogleEP
	}
	gs, err := s.loadGoogle(context.Background())
	if err != nil {
		s.log.Error("gmail: no se pudieron leer las credenciales guardadas en Ajustes", "err", err.Error())
	}
	switch {
	case gs != nil:
		s.gmailRT.Store(s.buildGmail(*gs, "ajustes"))
	case d.Google != nil:
		s.gmailRT.Store(s.buildGmail(*d.Google, "env"))
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	any := []string{"enroll", "mfa", "full"}

	mux.HandleFunc("GET /healthz", s.healthz)
	// Public artifact metadata only; clients must always consult the running server.
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, buildinfo.Current())
	})

	// Autenticación
	mux.HandleFunc("GET /api/auth/state", s.h(s.authState))
	mux.HandleFunc("POST /api/auth/register", s.h(s.register))
	mux.HandleFunc("POST /api/auth/login", s.h(s.login))
	mux.HandleFunc("POST /api/auth/logout", s.authed(any, s.logout))
	mux.HandleFunc("POST /api/auth/enroll/totp/begin", s.enrollOrStepUp(s.totpBegin))
	mux.HandleFunc("POST /api/auth/enroll/totp/finish", s.enrollOrStepUp(s.totpFinish))
	mux.HandleFunc("POST /api/auth/enroll/passkey/begin", s.enrollOrStepUp(s.passkeyRegisterBegin))
	mux.HandleFunc("POST /api/auth/enroll/passkey/finish", s.enrollOrStepUp(s.passkeyRegisterFinish))
	mux.HandleFunc("POST /api/auth/mfa/totp", s.authed([]string{"mfa"}, s.mfaTOTP))
	mux.HandleFunc("POST /api/auth/mfa/recovery", s.authed([]string{"mfa"}, s.mfaRecovery))
	mux.HandleFunc("POST /api/auth/mfa/passkey/begin", s.authed([]string{"mfa", "full"}, s.passkeyLoginBegin))
	mux.HandleFunc("POST /api/auth/mfa/passkey/finish", s.authed([]string{"mfa"}, s.mfaPasskeyFinish))
	mux.HandleFunc("POST /api/auth/step-up/totp", s.full(s.stepUpTOTP))
	mux.HandleFunc("POST /api/auth/step-up/passkey", s.full(s.stepUpPasskey))

	// Cuenta y seguridad
	mux.HandleFunc("GET /api/me", s.full(s.me))
	mux.HandleFunc("PATCH /api/me", s.full(s.updateMe))
	mux.HandleFunc("POST /api/me/password", s.withStepUp(s.changePassword))
	mux.HandleFunc("GET /api/security/sessions", s.full(s.listSessions))
	mux.HandleFunc("DELETE /api/security/sessions/{id}", s.full(s.revokeSession))
	mux.HandleFunc("GET /api/security/activity", s.full(s.activity))
	mux.HandleFunc("GET /api/security/passkeys", s.full(s.listPasskeys))
	mux.HandleFunc("DELETE /api/security/passkeys/{id}", s.withStepUp(s.deletePasskey))
	mux.HandleFunc("DELETE /api/security/totp", s.withStepUp(s.deleteTOTP))
	mux.HandleFunc("POST /api/security/recovery-codes", s.withStepUp(s.regenRecovery))

	// Finanzas
	// Importación de correos y reglas de categoría
	mux.HandleFunc("POST /api/import/eml", s.full(s.importEML))
	mux.HandleFunc("GET /api/merchant-rules", s.full(s.listRules))
	mux.HandleFunc("POST /api/merchant-rules", s.full(s.createRule))
	mux.HandleFunc("DELETE /api/merchant-rules/{id}", s.full(s.deleteRule))
	// Gmail
	mux.HandleFunc("GET /api/gmail", s.full(s.gmailStatus))
	mux.HandleFunc("POST /api/gmail/connect", s.withStepUp(s.gmailConnect))
	mux.HandleFunc("POST /api/gmail/callback", s.full(s.gmailCallback))
	mux.HandleFunc("POST /api/gmail/sync", s.full(s.gmailSync))
	mux.HandleFunc("PATCH /api/gmail", s.full(s.gmailOptions))
	mux.HandleFunc("PUT /api/gmail/gemini", s.withStepUp(s.gmailGemini))
	mux.HandleFunc("DELETE /api/gmail", s.full(s.gmailDisconnect))
	mux.HandleFunc("POST /api/gmail/webhook", s.gmailWebhook)
	mux.HandleFunc("GET /api/admin/gmail", s.full(s.adminGmail))
	mux.HandleFunc("PUT /api/admin/gmail", s.adminOnly(s.adminSetGmail))
	mux.HandleFunc("DELETE /api/admin/gmail", s.adminOnly(s.adminDeleteGmail))
	mux.HandleFunc("GET /api/push/vapid-key", s.full(s.pushKey))
	mux.HandleFunc("GET /api/push/subscriptions", s.full(s.pushSubscriptions))
	mux.HandleFunc("POST /api/push/subscribe", s.full(s.pushSubscribe))
	mux.HandleFunc("DELETE /api/push/subscribe", s.full(s.pushUnsubscribeCurrent))
	mux.HandleFunc("DELETE /api/push/subscriptions/{id}", s.full(s.pushUnsubscribe))
	mux.HandleFunc("GET /api/push/preferences", s.full(s.pushPreferences))
	mux.HandleFunc("PUT /api/push/preferences", s.full(s.pushUpdatePreferences))
	mux.HandleFunc("POST /api/push/test", s.full(s.pushTest))
	mux.HandleFunc("GET /api/accounts", s.full(s.listAccounts))
	mux.HandleFunc("POST /api/accounts", s.full(s.createAccount))
	mux.HandleFunc("PATCH /api/accounts/{id}", s.full(s.updateAccount))
	mux.HandleFunc("DELETE /api/accounts/{id}", s.full(s.deleteAccount))
	mux.HandleFunc("GET /api/categories", s.full(s.listCategories))
	mux.HandleFunc("POST /api/categories", s.full(s.createCategory))
	mux.HandleFunc("PATCH /api/categories/{id}", s.full(s.updateCategory))
	mux.HandleFunc("DELETE /api/categories/{id}", s.full(s.deleteCategory))
	mux.HandleFunc("GET /api/transactions", s.full(s.listTransactions))
	mux.HandleFunc("POST /api/transactions", s.full(s.createTransaction))
	mux.HandleFunc("GET /api/transactions/{id}", s.full(s.getTransaction))
	mux.HandleFunc("PATCH /api/transactions/{id}", s.full(s.updateTransaction))
	mux.HandleFunc("DELETE /api/transactions/{id}", s.full(s.deleteTransaction))
	mux.HandleFunc("POST /api/transactions/{id}/confirm", s.full(s.confirmTransaction))
	mux.HandleFunc("GET /api/budgets", s.full(s.listBudgets))
	mux.HandleFunc("POST /api/budgets", s.full(s.createBudget))
	mux.HandleFunc("PATCH /api/budgets/{id}", s.full(s.updateBudget))
	mux.HandleFunc("DELETE /api/budgets/{id}", s.full(s.deleteBudget))
	mux.HandleFunc("GET /api/budgets/summary", s.full(s.budgetSummary))
	mux.HandleFunc("GET /api/alerts", s.full(s.listAlerts))
	mux.HandleFunc("GET /api/dashboard", s.full(s.dashboard))
	mux.HandleFunc("GET /api/reports/summary", s.full(s.reportSummary))
	mux.HandleFunc("GET /api/reports/export.csv", s.withStepUp(s.reportExport))
	mux.HandleFunc("GET /api/reports/reconciliation", s.full(s.reconciliation))
	mux.HandleFunc("PUT /api/statements", s.full(s.putStatement))
	mux.HandleFunc("GET /api/recurring", s.full(s.listRecurring))
	mux.HandleFunc("POST /api/recurring", s.full(s.createRecurring))
	mux.HandleFunc("DELETE /api/recurring/{id}", s.full(s.deleteRecurring))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, errNotFound) })
	if s.static != nil {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
				return
			}
			s.static.ServeHTTP(w, r)
		})
	}
	return s.realIP(s.logRequests(s.securityHeaders(s.csrf(mux))))
}

// healthz lo consultan Docker, Uptime Kuma y deploy/smoke.sh. Falla si la base no responde
// o su esquema no es el que espera este binario (migración a medias, restauración antigua).
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	v, err := db.Version(r.Context(), s.store.DB)
	latest, lerr := db.Latest()
	out := map[string]any{"status": "ok", "schema": v, "gmail": s.gm() != nil, "push": s.push != nil}
	if err != nil || lerr != nil || v != latest {
		out["status"] = "error"
		writeJSON(w, 503, out)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) importer() *importer.Importer {
	return &importer.Importer{Store: s.store, Keys: s.keys, Clock: s.clock, DefaultTZ: s.cfg.DefaultTZ}
}

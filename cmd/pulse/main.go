// Pulse: registro de gastos personales.
//
//	pulse serve                       inicia el servidor
//	pulse migrate                     aplica migraciones
//	pulse invite create [--email e] [--role user|admin] [--ttl 72h]
//	pulse invite list | invite revoke <id>
//	pulse user list | user disable <email> | user enable <email> | user reset-mfa <email>
//	pulse genkeys --out <dir>         genera la clave maestra (KEK)
//	pulse healthcheck                 usado por Docker
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"pulse/internal/admin"
	"pulse/internal/api"
	"pulse/internal/auth"
	"pulse/internal/clock"
	"pulse/internal/config"
	"pulse/internal/crypto"
	"pulse/internal/db"
	"pulse/internal/gmail"
	"pulse/internal/importer"
	"pulse/internal/mailparse"
	"pulse/internal/push"
	"pulse/internal/store"
	"pulse/internal/webui"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "migrate":
		err = withApp(func(a *app) error { fmt.Println("Migraciones aplicadas."); return nil })
	case "invite":
		err = inviteCmd(os.Args[2:])
	case "user":
		err = userCmd(os.Args[2:])
	case "genkeys":
		err = genkeys(os.Args[2:])
	case "parse":
		err = parseCmd(os.Args[2:])
	case "vapid":
		err = vapidKeys(os.Args[2:])
	case "healthcheck":
		err = healthcheck()
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `uso: pulse <serve|migrate|invite|user|genkeys|vapid|healthcheck>
  invite create [--email correo] [--role user|admin] [--ttl 72h]
  invite list
  invite revoke <id>
  user list | disable <email> | enable <email> | reset-mfa <email>
  genkeys --out <directorio>
  vapid --out <directorio>
  parse [--texto] [--titular "NOMBRE"] correo.eml…`)
	os.Exit(2)
}

type app struct {
	cfg   *config.Config
	store *store.Store
	keys  *crypto.Keyring
}

func withApp(fn func(*app) error) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("configuración inválida:\n%w", err)
	}
	keys, err := crypto.LoadKEKFile(cfg.KEKFile)
	if err != nil {
		return err
	}
	d, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := db.Migrate(context.Background(), d); err != nil {
		return err
	}
	return fn(&app{cfg: cfg, store: &store.Store{DB: d, Clock: clock.Real{}}, keys: keys})
}

func serve() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return withApp(func(a *app) error {
		var notifications *push.Service
		if a.cfg.VAPIDPublicKey != "" {
			sender, err := push.NewSender(a.cfg.VAPIDPublicKey, a.cfg.VAPIDPrivateFile, a.cfg.VAPIDSubject)
			if err != nil {
				return err
			}
			notifications = &push.Service{Store: a.store, Keys: a.keys, Sender: sender, Log: log}
		}
		syncer, verifier, err := gmailSetup(a, log)
		if err != nil {
			return err
		}
		srv := api.New(api.Deps{Config: a.cfg, Store: a.store, Keys: a.keys, Hasher: &auth.Hasher{Keys: a.keys},
			Clock: clock.Real{}, Log: log, Static: webui.Handler(), Push: notifications, Gmail: syncer, PubSub: verifier})
		hs := &http.Server{
			Addr: a.cfg.Listen, Handler: srv.Handler(),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
			IdleTimeout: 120 * time.Second, MaxHeaderBytes: 32 << 10,
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		pushDone := make(chan struct{})
		if notifications != nil {
			go func() { defer close(pushDone); notifications.Run(ctx) }()
		} else {
			close(pushDone)
		}
		gmailDone := make(chan struct{})
		if syncer != nil {
			syncer.OnImported, syncer.OnBroken = srv.AfterImport, srv.GmailBroken
			go func() { defer close(gmailDone); syncer.Run(ctx) }()
		} else {
			close(gmailDone)
		}
		defer func() { stop(); <-pushDone; <-gmailDone }()
		go func() { // limpieza periódica de sesiones y desafíos caducados
			t := time.NewTicker(10 * time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := a.store.PurgeExpiredSessions(ctx); err != nil {
						log.Error("limpiando sesiones", "err", err.Error())
					}
				}
			}
		}()
		errc := make(chan error, 1)
		go func() { errc <- hs.ListenAndServe() }()
		log.Info("Pulse escuchando", "addr", a.cfg.Listen, "origin", a.cfg.Origin(), "env", a.cfg.Env)
		select {
		case err := <-errc:
			return err
		case <-ctx.Done():
		}
		log.Info("apagando…")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return hs.Shutdown(sctx)
	})
}

func vapidKeys(args []string) error {
	fs := flag.NewFlagSet("vapid", flag.ContinueOnError)
	out := fs.String("out", "./secrets", "directorio de salida")
	if err := fs.Parse(args); err != nil {
		return err
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	p := filepath.Join(*out, "vapid_private")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creando clave VAPID (no se sobrescriben claves existentes): %w", err)
	}
	if _, err := f.WriteString(private + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("Clave privada en %s\nPULSE_VAPID_PUBLIC_KEY=%s\n", p, public)
	return nil
}

func inviteCmd(args []string) error {
	if len(args) == 0 {
		usage()
	}
	return withApp(func(a *app) error {
		ctx := context.Background()
		switch args[0] {
		case "create":
			fs := flag.NewFlagSet("invite create", flag.ContinueOnError)
			email := fs.String("email", "", "atar la invitación a este email (opcional)")
			role := fs.String("role", "user", "user | admin")
			ttl := fs.Duration("ttl", 72*time.Hour, "vigencia")
			if err := fs.Parse(args[1:]); err != nil {
				return err
			}
			code, id, err := admin.CreateInvite(ctx, a.store, a.keys, *email, *role, *ttl)
			if err != nil {
				return err
			}
			a.store.Audit(ctx, "", "invite_created", "cli", "", map[string]string{"invite": id, "role": *role})
			fmt.Printf("Invitación creada (id %s)\n\n    %s\n\nVálida hasta %s, un solo uso. Este código no se volverá a mostrar.\n",
				id, code, time.Now().Add(*ttl).Format("2006-01-02 15:04"))
			return nil
		case "list":
			inv, err := a.store.ListInvites(ctx)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tEMAIL\tROL\tESTADO\tVENCE")
			for _, i := range inv {
				st := "pendiente"
				switch {
				case i.UsedAt != nil:
					st = "usada"
				case i.RevokedAt != nil:
					st = "revocada"
				case time.Now().After(i.ExpiresAt):
					st = "caducada"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", i.ID, i.Email, i.Role, st, i.ExpiresAt.Local().Format("2006-01-02 15:04"))
			}
			return tw.Flush()
		case "revoke":
			if len(args) < 2 {
				return errors.New("falta el id")
			}
			if err := a.store.RevokeInvite(ctx, args[1]); err != nil {
				return fmt.Errorf("no se pudo revocar (¿ya usada o inexistente?): %w", err)
			}
			fmt.Println("Invitación revocada.")
			return nil
		}
		usage()
		return nil
	})
}

func userCmd(args []string) error {
	if len(args) == 0 {
		usage()
	}
	return withApp(func(a *app) error {
		ctx := context.Background()
		switch args[0] {
		case "list":
			us, err := a.store.ListUsers(ctx)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "EMAIL\tROL\tESTADO\tCREADO")
			for _, u := range us {
				st := "activo"
				if u.DisabledAt != nil {
					st = "deshabilitado"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.Email, u.Role, st, u.CreatedAt.Local().Format("2006-01-02"))
			}
			return tw.Flush()
		case "disable", "enable", "reset-mfa":
			if len(args) < 2 {
				return errors.New("falta el email")
			}
			u, err := a.store.UserByEmail(ctx, args[1])
			if err != nil {
				return fmt.Errorf("usuario no encontrado")
			}
			switch args[0] {
			case "disable":
				_, err = a.store.SetDisabled(ctx, u.Email, true)
			case "enable":
				_, err = a.store.SetDisabled(ctx, u.Email, false)
			case "reset-mfa":
				err = a.store.ResetMFA(ctx, u.ID)
			}
			if err != nil {
				return err
			}
			a.store.Audit(ctx, u.ID, "admin_"+args[0], "cli", "", nil)
			fmt.Println("Listo.")
			return nil
		}
		usage()
		return nil
	})
}

func genkeys(args []string) error {
	fs := flag.NewFlagSet("genkeys", flag.ContinueOnError)
	out := fs.String("out", "./secrets", "directorio de salida")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	p := filepath.Join(*out, "pulse_kek")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s ya existe: no se sobrescribe (perderías acceso a los datos cifrados)", p)
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(crypto.GenerateKEK() + "\n"); err != nil {
		return err
	}
	fmt.Printf("Clave maestra creada en %s\nGuarda una copia en tu gestor de contraseñas: sin ella no se pueden leer los datos ni los backups.\n", p)
	return nil
}

func healthcheck() error {
	addr := os.Getenv("PULSE_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("PULSE_LISTEN inválido: %w", err)
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("estado %d", resp.StatusCode)
	}
	return nil
}

// parseCmd analiza correos .eml sin base de datos ni clave: sirve para validar los lectores
// con correos reales. Con --texto muestra el contenido ya anonimizado, para compartirlo
// como ejemplo sin exponer nombre, emails ni números largos.
func parseCmd(args []string) error {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	showText := fs.Bool("texto", false, "mostrar también el texto extraído (anonimizado)")
	holder := fs.String("titular", "", "nombre del titular, para quitarlo del texto")
	tz := fs.String("tz", "America/Guayaquil", "zona horaria del banco")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("indica uno o más archivos .eml (en Gmail: ⋮ → Descargar mensaje)")
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for _, path := range fs.Args() {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		e, err := mailparse.ParseEML(f)
		f.Close()
		if err != nil {
			fmt.Printf("%s: %v\n", path, err)
			continue
		}
		out := map[string]any{"archivo": filepath.Base(path), "remitente": e.From, "asunto": mailparse.Redact(e.Subject, *holder), "resultado": mailparse.Analyze(e, loc)}
		if *showText {
			out["texto_anonimizado"] = strings.Split(mailparse.Redact(e.Text, *holder), "\n")
		}
		enc.Encode(out)
	}
	return nil
}

// gmailSetup prepara la integración con Gmail si hay credenciales de Google.
func gmailSetup(a *app, log *slog.Logger) (*gmail.Syncer, *gmail.PubSubVerifier, error) {
	if !a.cfg.GmailEnabled() {
		return nil, nil, nil
	}
	secret, err := readSecret(a.cfg.GoogleClientSecretFile)
	if err != nil {
		return nil, nil, fmt.Errorf("leyendo el secreto OAuth de Google: %w", err)
	}
	ep := gmail.GoogleEndpoints()
	if a.cfg.GoogleFakeURL != "" {
		ep = gmail.FakeEndpoints(a.cfg.GoogleFakeURL)
		log.Warn("Gmail usa un servidor FALSO de Google (solo pruebas)", "url", a.cfg.GoogleFakeURL)
	}
	httpc := gmail.NewHTTPClient()
	client := &gmail.Client{ClientID: a.cfg.GoogleClientID, ClientSecret: secret, RedirectURL: a.cfg.Origin() + "/ajustes/gmail", EP: ep, HTTP: httpc}
	s := &gmail.Syncer{Store: a.store, Keys: a.keys, Client: client, Clock: clock.Real{}, Log: log,
		Interval: a.cfg.GmailPollInterval, Topic: a.cfg.GmailPubSubTopic,
		Importer: &importer.Importer{Store: a.store, Keys: a.keys, Clock: clock.Real{}, DefaultTZ: a.cfg.DefaultTZ}}
	if a.cfg.GeminiKeyFile != "" {
		key, err := readSecret(a.cfg.GeminiKeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("leyendo la clave de Gemini: %w", err)
		}
		s.Gemini = &gmail.Gemini{Key: key, Model: a.cfg.GeminiModel, Base: ep.Gemini, HTTP: httpc}
	}
	var v *gmail.PubSubVerifier
	if a.cfg.GmailPubSubTopic != "" {
		v = &gmail.PubSubVerifier{CertsURL: ep.Certs, Audience: a.cfg.GmailPubSubAudience, ServiceAccount: a.cfg.GmailPubSubSA, HTTP: httpc, Now: time.Now}
	}
	return s, v, nil
}

func readSecret(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", errors.New("el archivo está vacío")
	}
	return v, nil
}

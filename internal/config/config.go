// Package config carga y valida la configuración desde variables de entorno.
// Falla rápido: si algo falta o es inválido, el proceso no arranca.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // zona horaria embebida: la imagen no depende del sistema
)

type Config struct {
	Env            string // production | development | test
	BaseURL        *url.URL
	Listen         string
	DBPath         string
	KEKFile        string
	TrustedProxies []*net.IPNet
	DefaultTZ      string
	// RegisterPerHour limita los intentos de registro por IP. Solo se sube para los tests E2E.
	RegisterPerHour  int
	VAPIDPublicKey   string
	VAPIDPrivateFile string
	VAPIDSubject     string

	// Gmail (fase 7). Todo opcional: sin client ID la integración queda desactivada.
	GoogleClientID         string
	GoogleClientSecretFile string
	GmailPollInterval      time.Duration
	GmailPubSubTopic       string // projects/<proyecto>/topics/<tema>; vacío = solo polling
	GmailPubSubSA          string // cuenta de servicio que firma los avisos de Pub/Sub
	GmailPubSubAudience    string // "aud" esperado en el JWT de Pub/Sub
	GeminiKeyFile          string // vacío = Gemini no disponible
	GeminiModel            string
	// GoogleFakeURL reemplaza todos los servicios de Google por un servidor falso (solo E2E, nunca en producción).
	GoogleFakeURL string
}

// GmailEnabled indica si el servidor tiene credenciales OAuth de Google.
func (c *Config) GmailEnabled() bool { return c.GoogleClientID != "" }

func (c *Config) Production() bool { return c.Env == "production" }

// Origin devuelve "https://host[:puerto]" sin barra final.
func (c *Config) Origin() string { return c.BaseURL.Scheme + "://" + c.BaseURL.Host }

// RPID es el identificador WebAuthn (el host sin puerto).
func (c *Config) RPID() string { return c.BaseURL.Hostname() }

// Load lee la configuración. getenv suele ser os.Getenv; los tests pasan un mapa.
func Load(getenv func(string) string) (*Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	var errs []error
	c := &Config{
		Env:              get("PULSE_ENV", "production"),
		Listen:           get("PULSE_LISTEN", ":8080"),
		DBPath:           get("PULSE_DB_PATH", "/data/pulse.db"),
		KEKFile:          get("PULSE_KEK_FILE", "/run/secrets/pulse_kek"),
		DefaultTZ:        get("TZ", "America/Guayaquil"),
		VAPIDPublicKey:   get("PULSE_VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateFile: get("PULSE_VAPID_PRIVATE_FILE", ""),
		VAPIDSubject:     get("PULSE_VAPID_SUBJECT", ""),

		GoogleClientID:         get("PULSE_GOOGLE_CLIENT_ID", ""),
		GoogleClientSecretFile: get("PULSE_GOOGLE_CLIENT_SECRET_FILE", ""),
		GmailPubSubTopic:       get("PULSE_GMAIL_PUBSUB_TOPIC", ""),
		GmailPubSubSA:          get("PULSE_GMAIL_PUBSUB_SA", ""),
		GmailPubSubAudience:    get("PULSE_GMAIL_PUBSUB_AUDIENCE", ""),
		GeminiKeyFile:          get("PULSE_GEMINI_KEY_FILE", ""),
		GeminiModel:            get("PULSE_GEMINI_MODEL", "gemini-2.5-flash"),
		GoogleFakeURL:          get("PULSE_GOOGLE_FAKE_URL", ""),
	}
	if c.VAPIDPublicKey != "" || c.VAPIDPrivateFile != "" || c.VAPIDSubject != "" {
		if c.VAPIDPublicKey == "" || c.VAPIDPrivateFile == "" || c.VAPIDSubject == "" {
			errs = append(errs, errors.New("push requiere PULSE_VAPID_PUBLIC_KEY, PULSE_VAPID_PRIVATE_FILE y PULSE_VAPID_SUBJECT"))
		} else if u, err := url.Parse(c.VAPIDSubject); err != nil || !(u.Scheme == "mailto" && strings.Contains(u.Opaque, "@") || u.Scheme == "https" && u.Host != "") {
			errs = append(errs, errors.New("PULSE_VAPID_SUBJECT debe ser mailto:correo o una URL https"))
		}
	}
	c.RegisterPerHour = 5
	if v := get("PULSE_REGISTER_PER_HOUR", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 10000 {
			errs = append(errs, fmt.Errorf("PULSE_REGISTER_PER_HOUR inválido: %q", v))
		} else if c.Env == "production" && n > 20 {
			errs = append(errs, errors.New("PULSE_REGISTER_PER_HOUR no puede pasar de 20 en producción"))
		} else {
			c.RegisterPerHour = n
		}
	}
	switch c.Env {
	case "production", "development", "test":
	default:
		errs = append(errs, fmt.Errorf("PULSE_ENV inválido %q (production|development|test)", c.Env))
	}

	raw := get("PULSE_BASE_URL", "")
	if raw == "" {
		errs = append(errs, errors.New("PULSE_BASE_URL es obligatorio (ej. https://pulse.espectro.uk)"))
	} else if u, err := url.Parse(raw); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		errs = append(errs, fmt.Errorf("PULSE_BASE_URL inválido: %q", raw))
	} else {
		if u.Scheme != "https" && c.Env == "production" {
			errs = append(errs, errors.New("PULSE_BASE_URL debe usar https en producción"))
		}
		if u.Path != "" && u.Path != "/" {
			errs = append(errs, errors.New("PULSE_BASE_URL no debe tener ruta"))
		}
		u.Path = ""
		c.BaseURL = u
	}

	if _, err := time.LoadLocation(c.DefaultTZ); err != nil {
		errs = append(errs, fmt.Errorf("TZ inválida %q", c.DefaultTZ))
	}

	for _, cidr := range strings.Split(get("PULSE_TRUSTED_PROXY", ""), ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			errs = append(errs, fmt.Errorf("PULSE_TRUSTED_PROXY inválido: %q", cidr))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, n)
	}
	errs = append(errs, c.loadGmail(get)...)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return c, nil
}

var (
	reTopic = regexp.MustCompile(`^projects/[a-z][a-z0-9-]{4,28}[a-z0-9]/topics/[A-Za-z][\w.~+%-]{2,254}$`)
	reSA    = regexp.MustCompile(`^[a-z0-9-]+@[a-z0-9-]+\.iam\.gserviceaccount\.com$`)
	reModel = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,60}$`)
	reOAuth = regexp.MustCompile(`^[\w.-]{1,200}\.apps\.googleusercontent\.com$`)
)

// ValidGoogleClientID, ValidPubSubTopic y ValidServiceAccount validan lo que un admin
// escribe en Ajustes → Gmail (las variables de entorno usan las mismas reglas).
func ValidGoogleClientID(v string) bool { return reOAuth.MatchString(v) }
func ValidPubSubTopic(v string) bool    { return reTopic.MatchString(v) }
func ValidServiceAccount(v string) bool { return reSA.MatchString(v) }

func (c *Config) loadGmail(get func(k, def string) string) []error {
	var errs []error
	if (c.GoogleClientID == "") != (c.GoogleClientSecretFile == "") {
		errs = append(errs, errors.New("Gmail requiere PULSE_GOOGLE_CLIENT_ID y PULSE_GOOGLE_CLIENT_SECRET_FILE juntos"))
	}
	c.GmailPollInterval = 15 * time.Minute
	if v := get("PULSE_GMAIL_POLL_INTERVAL", ""); v != "" {
		d, err := time.ParseDuration(v)
		min := time.Minute
		if c.Env == "production" {
			min = 5 * time.Minute // la cuota de Gmail es por usuario: no tiene sentido ir más rápido
		}
		if err != nil || d < min || d > 24*time.Hour {
			errs = append(errs, fmt.Errorf("PULSE_GMAIL_POLL_INTERVAL inválido %q (entre %s y 24h)", v, min))
		} else {
			c.GmailPollInterval = d
		}
	}
	if c.GmailPubSubTopic != "" {
		if !reTopic.MatchString(c.GmailPubSubTopic) {
			errs = append(errs, errors.New("PULSE_GMAIL_PUBSUB_TOPIC debe ser projects/<proyecto>/topics/<tema>"))
		}
		if !reSA.MatchString(c.GmailPubSubSA) {
			errs = append(errs, errors.New("con Pub/Sub, PULSE_GMAIL_PUBSUB_SA debe ser la cuenta de servicio (…@….iam.gserviceaccount.com) que firma los avisos"))
		}
		if c.GoogleClientID == "" {
			errs = append(errs, errors.New("PULSE_GMAIL_PUBSUB_TOPIC necesita las credenciales OAuth de Google"))
		}
	}
	if c.GmailPubSubAudience == "" && c.BaseURL != nil {
		c.GmailPubSubAudience = c.Origin() + "/api/gmail/webhook"
	}
	if !reModel.MatchString(c.GeminiModel) {
		errs = append(errs, fmt.Errorf("PULSE_GEMINI_MODEL inválido %q", c.GeminiModel))
	}
	if c.GoogleFakeURL != "" {
		if c.Env == "production" {
			errs = append(errs, errors.New("PULSE_GOOGLE_FAKE_URL no se permite en producción"))
		} else if u, err := url.Parse(c.GoogleFakeURL); err != nil || u.Host == "" {
			errs = append(errs, fmt.Errorf("PULSE_GOOGLE_FAKE_URL inválido: %q", c.GoogleFakeURL))
		}
	}
	return errs
}

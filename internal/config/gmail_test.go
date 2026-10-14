package config

import (
	"strings"
	"testing"
	"time"
)

func TestGmailConfig(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		m := map[string]string{"PULSE_BASE_URL": "https://pulse.espectro.uk"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	c, err := Load(env(base(nil)))
	if err != nil || c.GmailEnabled() || c.GmailPollInterval != 15*time.Minute {
		t.Fatalf("sin credenciales Gmail queda apagado: %+v %v", c, err)
	}
	c, err = Load(env(base(map[string]string{"PULSE_GOOGLE_CLIENT_ID": "id", "PULSE_GOOGLE_CLIENT_SECRET_FILE": "/run/secrets/google",
		"PULSE_GMAIL_PUBSUB_TOPIC": "projects/pulse-123456/topics/gmail", "PULSE_GMAIL_PUBSUB_SA": "push@pulse-123456.iam.gserviceaccount.com"})))
	if err != nil || !c.GmailEnabled() || c.GmailPubSubAudience != "https://pulse.espectro.uk/api/gmail/webhook" {
		t.Fatalf("%+v %v", c, err)
	}
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"id sin secreto":                   {map[string]string{"PULSE_GOOGLE_CLIENT_ID": "id"}, "juntos"},
		"intervalo corto":                  {map[string]string{"PULSE_GMAIL_POLL_INTERVAL": "30s"}, "PULSE_GMAIL_POLL_INTERVAL"},
		"intervalo de 1 min en producción": {map[string]string{"PULSE_GMAIL_POLL_INTERVAL": "1m"}, "PULSE_GMAIL_POLL_INTERVAL"},
		"tema mal escrito":                 {map[string]string{"PULSE_GOOGLE_CLIENT_ID": "id", "PULSE_GOOGLE_CLIENT_SECRET_FILE": "f", "PULSE_GMAIL_PUBSUB_TOPIC": "gmail", "PULSE_GMAIL_PUBSUB_SA": "a@b.iam.gserviceaccount.com"}, "PULSE_GMAIL_PUBSUB_TOPIC"},
		"Pub/Sub sin cuenta":               {map[string]string{"PULSE_GOOGLE_CLIENT_ID": "id", "PULSE_GOOGLE_CLIENT_SECRET_FILE": "f", "PULSE_GMAIL_PUBSUB_TOPIC": "projects/pulse-123456/topics/gmail"}, "PULSE_GMAIL_PUBSUB_SA"},
		"Google falso en producción":       {map[string]string{"PULSE_GOOGLE_FAKE_URL": "http://localhost:9"}, "no se permite en producción"},
		"modelo raro":                      {map[string]string{"PULSE_GEMINI_MODEL": "../../x"}, "PULSE_GEMINI_MODEL"},
	}
	for name, tc := range cases {
		_, err := Load(env(base(tc.env)))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, esperaba %q", name, err, tc.want)
		}
	}
	c, err = Load(env(base(map[string]string{"PULSE_ENV": "development", "PULSE_BASE_URL": "http://localhost:8080", "PULSE_GMAIL_POLL_INTERVAL": "1m", "PULSE_GOOGLE_FAKE_URL": "http://localhost:9999"})))
	if err != nil || c.GmailPollInterval != time.Minute {
		t.Fatalf("en desarrollo se permite 1 min y Google falso: %v", err)
	}
}

package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadOK(t *testing.T) {
	c, err := Load(env(map[string]string{
		"PULSE_BASE_URL":      "https://pulse.espectro.uk",
		"PULSE_TRUSTED_PROXY": "172.18.0.0/16",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Origin() != "https://pulse.espectro.uk" || c.RPID() != "pulse.espectro.uk" {
		t.Fatalf("origen/RPID incorrectos: %s %s", c.Origin(), c.RPID())
	}
	if !c.Production() || c.DBPath != "/data/pulse.db" || len(c.TrustedProxies) != 1 {
		t.Fatalf("valores por defecto incorrectos: %+v", c)
	}
}

func TestLoadErrores(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"sin base url", map[string]string{}, "PULSE_BASE_URL es obligatorio"},
		{"http en producción", map[string]string{"PULSE_BASE_URL": "http://pulse.espectro.uk"}, "https en producción"},
		{"url con ruta", map[string]string{"PULSE_BASE_URL": "https://x.com/app"}, "no debe tener ruta"},
		{"url basura", map[string]string{"PULSE_BASE_URL": "::::"}, "inválido"},
		{"entorno raro", map[string]string{"PULSE_BASE_URL": "https://x.com", "PULSE_ENV": "staging"}, "PULSE_ENV"},
		{"tz inválida", map[string]string{"PULSE_BASE_URL": "https://x.com", "TZ": "Marte/Base"}, "TZ inválida"},
		{"límite de registro alto en producción", map[string]string{"PULSE_BASE_URL": "https://x.com", "PULSE_REGISTER_PER_HOUR": "1000"}, "no puede pasar de 20"},
		{"límite de registro basura", map[string]string{"PULSE_BASE_URL": "https://x.com", "PULSE_REGISTER_PER_HOUR": "x"}, "PULSE_REGISTER_PER_HOUR"},
		{"proxy inválido", map[string]string{"PULSE_BASE_URL": "https://x.com", "PULSE_TRUSTED_PROXY": "1.2.3"}, "PULSE_TRUSTED_PROXY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperaba error con %q, obtuve %v", tc.want, err)
			}
		})
	}
}

func TestHTTPPermitidoEnDesarrollo(t *testing.T) {
	_, err := Load(env(map[string]string{"PULSE_BASE_URL": "http://localhost:8080", "PULSE_ENV": "development"}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestReportaTodosLosErrores(t *testing.T) {
	_, err := Load(env(map[string]string{"PULSE_ENV": "x", "TZ": "nope"}))
	if err == nil || strings.Count(err.Error(), "\n") < 2 {
		t.Fatalf("debe reportar todos los errores juntos: %v", err)
	}
}

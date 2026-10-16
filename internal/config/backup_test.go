package config

import (
	"strings"
	"testing"
	"time"
)

const ageKey = "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"

func TestRespaldoExterno(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		m := map[string]string{"PULSE_BASE_URL": "https://pulse.espectro.uk", "PULSE_DB_PATH": "/data/pulse.db"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	c, err := Load(env(base(nil)))
	if err != nil || c.BackupTarget != "" || c.BackupDir != "/data/backups-externos" {
		t.Fatalf("desactivado por defecto: %v %+v", err, c)
	}
	c, err = Load(env(base(map[string]string{
		"PULSE_BACKUP_TARGET": "s3", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_S3_ENDPOINT": "https://s3.us-east-1.amazonaws.com",
		"PULSE_BACKUP_S3_BUCKET": "mis-respaldos", "PULSE_BACKUP_S3_PREFIX": "pulse", "PULSE_BACKUP_S3_ACCESS_KEY_ID": "AKIA",
		"PULSE_BACKUP_S3_SECRET_FILE": "/run/secrets/s3", "PULSE_BACKUP_INTERVAL": "6h",
	})))
	if err != nil || c.BackupS3Prefix != "pulse/" || c.BackupInterval != 6*time.Hour || !c.BackupS3PathStyle || c.BackupS3Region != "us-east-1" {
		t.Fatalf("S3: %v %+v", err, c)
	}
	c, err = Load(env(base(map[string]string{"PULSE_BACKUP_TARGET": "webdav", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey,
		"PULSE_BACKUP_WEBDAV_URL": "https://nube.ejemplo/remote.php/dav/files/ana/pulse", "PULSE_BACKUP_WEBDAV_USER": "ana",
		"PULSE_BACKUP_WEBDAV_PASSWORD_FILE": "/run/secrets/dav", "PULSE_BACKUP_INTERVAL": "manual"})))
	if err != nil || c.BackupInterval != 0 || c.BackupWebDAVURL.Host != "nube.ejemplo" {
		t.Fatalf("WebDAV: %v %+v", err, c)
	}
	// Solo con la clave pública: respaldo local en /backups (montado desde /opt/backup/pulse).
	c, err = Load(env(base(map[string]string{"PULSE_BACKUP_AGE_RECIPIENTS": ageKey})))
	if err != nil || !c.BackupEnabled || c.BackupLocalDir != "/backups" || c.BackupLocalKeep != 30 || len(c.BackupTargets) != 0 {
		t.Fatalf("local por defecto: %v %+v", err, c)
	}
	c, err = Load(env(base(map[string]string{"PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_TARGET": "gdrive",
		"PULSE_BACKUP_GDRIVE_CLIENT_ID": "id.apps.googleusercontent.com", "PULSE_BACKUP_GDRIVE_CLIENT_SECRET_FILE": "/run/secrets/g",
		"PULSE_BACKUP_GDRIVE_TOKEN_FILE": "/run/secrets/t", "PULSE_BACKUP_LOCAL_KEEP": "10"})))
	if err != nil || c.BackupGDriveFolder != "Backups/pulse" || c.BackupGDriveKeep != 30 || c.BackupLocalKeep != 10 ||
		len(c.BackupTargets) != 1 || c.BackupTargets[0] != "gdrive" || c.BackupLocalDir != "/backups" {
		t.Fatalf("Drive + local: %v %+v", err, c)
	}
	c, err = Load(env(base(map[string]string{"PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_LOCAL_DIR": "off", "PULSE_BACKUP_TARGET": "webdav,gdrive",
		"PULSE_BACKUP_WEBDAV_URL": "https://nube.ejemplo/dav", "PULSE_BACKUP_WEBDAV_USER": "a", "PULSE_BACKUP_WEBDAV_PASSWORD_FILE": "/x",
		"PULSE_BACKUP_GDRIVE_CLIENT_ID": "id", "PULSE_BACKUP_GDRIVE_CLIENT_SECRET_FILE": "/g", "PULSE_BACKUP_GDRIVE_TOKEN_FILE": "/t", "PULSE_BACKUP_GDRIVE_FOLDER": "/Respaldos/Pulse 2026/"})))
	if err != nil || c.BackupLocalDir != "" || len(c.BackupTargets) != 2 || c.BackupGDriveFolder != "Respaldos/Pulse 2026" {
		t.Fatalf("varios destinos: %v %+v", err, c)
	}
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"PULSE_BACKUP_TARGET": "ftp"}, "PULSE_BACKUP_TARGET inválido"},
		{map[string]string{"PULSE_BACKUP_TARGET": "webdav"}, "PULSE_BACKUP_AGE_RECIPIENTS"},
		{map[string]string{"PULSE_BACKUP_TARGET": "webdav", "PULSE_BACKUP_AGE_RECIPIENTS": "AGE-SECRET-KEY-1XXXX"}, "no es una clave pública"},
		{map[string]string{"PULSE_BACKUP_TARGET": "webdav", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_WEBDAV_URL": "http://nube.ejemplo/dav", "PULSE_BACKUP_WEBDAV_USER": "a", "PULSE_BACKUP_WEBDAV_PASSWORD_FILE": "/x"}, "https en producción"},
		{map[string]string{"PULSE_BACKUP_TARGET": "webdav", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_WEBDAV_URL": "https://a:b@nube.ejemplo/dav", "PULSE_BACKUP_WEBDAV_USER": "a", "PULSE_BACKUP_WEBDAV_PASSWORD_FILE": "/x"}, "no debe llevar usuario"},
		{map[string]string{"PULSE_BACKUP_TARGET": "webdav", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_WEBDAV_URL": "https://nube.ejemplo/dav"}, "PULSE_BACKUP_WEBDAV_PASSWORD_FILE"},
		{map[string]string{"PULSE_BACKUP_TARGET": "s3", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_S3_ENDPOINT": "https://s3.x", "PULSE_BACKUP_S3_BUCKET": "B!"}, "PULSE_BACKUP_S3_BUCKET inválido"},
		{map[string]string{"PULSE_BACKUP_TARGET": "s3", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_S3_ENDPOINT": "https://s3.x", "PULSE_BACKUP_S3_BUCKET": "bucket"}, "PULSE_BACKUP_S3_SECRET_FILE"},
		{map[string]string{"PULSE_BACKUP_TARGET": "s3", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_INTERVAL": "1m"}, "PULSE_BACKUP_INTERVAL inválido"},
		{map[string]string{"PULSE_BACKUP_TARGET": "gdrive", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey}, "PULSE_BACKUP_GDRIVE_TOKEN_FILE"},
		{map[string]string{"PULSE_BACKUP_TARGET": "gdrive", "PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_GDRIVE_FOLDER": "Back'ups"}, "PULSE_BACKUP_GDRIVE_FOLDER inválido"},
		{map[string]string{"PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_LOCAL_KEEP": "-1"}, "PULSE_BACKUP_LOCAL_KEEP inválido"},
		{map[string]string{"PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_LOCAL_DIR": "relativa"}, "ruta absoluta"},
		{map[string]string{"PULSE_BACKUP_AGE_RECIPIENTS": ageKey, "PULSE_BACKUP_LOCAL_DIR": "off"}, "no tiene destino"},
	}
	for _, tc := range cases {
		_, err := Load(env(base(tc.env)))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %v (esperaba %q)", tc.env, err, tc.want)
		}
	}
}

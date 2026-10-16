// Package buildinfo dice qué compilación está corriendo, para comprobar tras cada despliegue
// que el servidor tiene la versión nueva. El Dockerfile fija los valores con -ldflags -X.
package buildinfo

import (
	"os"
	"runtime/debug"
	"time"
)

// Fijados al compilar: -X pulse/internal/buildinfo.Time=2026-10-16T06:18:00Z -X pulse/internal/buildinfo.Commit=<sha>
var (
	Time   string
	Commit string
)

// Info es lo que ve el usuario: la hora de compilación (UTC) y el commit corto, si se conocen.
type Info struct {
	BuiltAt string `json:"built_at,omitempty"`
	Commit  string `json:"commit,omitempty"`
}

// Get usa los valores fijados al compilar; si faltan (go run, go build sin flags), los que Go
// guarda del repositorio y, como último recurso, la fecha del binario.
func Get() Info {
	i := Info{BuiltAt: normalize(Time), Commit: Commit}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch {
			case s.Key == "vcs.revision" && i.Commit == "":
				i.Commit = s.Value
			case s.Key == "vcs.time" && i.BuiltAt == "":
				i.BuiltAt = normalize(s.Value)
			}
		}
	}
	if i.BuiltAt == "" {
		if exe, err := os.Executable(); err == nil {
			if st, err := os.Stat(exe); err == nil {
				i.BuiltAt = st.ModTime().UTC().Format(time.RFC3339)
			}
		}
	}
	if len(i.Commit) > 7 {
		i.Commit = i.Commit[:7]
	}
	return i
}

// normalize deja solo fechas RFC 3339 válidas, en UTC; lo demás se descarta.
func normalize(v string) string {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

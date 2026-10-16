// Package buildinfo identifies the artifact actually running on the server.
package buildinfo

import "time"

// Set once at build time with -ldflags -X, never from the server's startup clock.
var BuiltAt string
var Revision string

type Info struct {
	BuiltAt  string `json:"built_at"`
	Revision string `json:"revision"`
}

func Current() Info {
	info := Info{Revision: Revision}
	if t, err := time.Parse(time.RFC3339, BuiltAt); err == nil {
		info.BuiltAt = t.UTC().Format(time.RFC3339)
	}
	return info
}

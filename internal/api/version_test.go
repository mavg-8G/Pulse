package api_test

import (
	"testing"

	"pulse/internal/buildinfo"
)

func TestVersionPublicAndNeverCached(t *testing.T) {
	e := newEnv(t)
	r := e.client().must(200, "GET", "/api/version", nil)
	info := buildinfo.Current()
	if len(r.Body) != 2 || r.str("built_at") != info.BuiltAt || r.str("revision") != info.Revision {
		t.Fatalf("public endpoint must expose only artifact metadata: %s", r.Raw)
	}
	if r.Headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("the running version must not be cached: %v", r.Headers)
	}
}

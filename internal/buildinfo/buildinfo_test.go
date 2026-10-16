package buildinfo

import "testing"

func TestGet(t *testing.T) {
	Time, Commit = "2026-10-16T01:18:00-05:00", "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { Time, Commit = "", "" })
	got := Get()
	if got.BuiltAt != "2026-10-16T06:18:00Z" || got.Commit != "0123456" {
		t.Fatalf("Get() = %+v", got)
	}
}

func TestGetFallback(t *testing.T) {
	Time, Commit = "no es una fecha", ""
	t.Cleanup(func() { Time = "" })
	if got := Get(); got.BuiltAt == "" || got.BuiltAt == Time {
		t.Fatalf("sin fecha válida debe caer a la del binario: %+v", got)
	}
}

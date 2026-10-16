package buildinfo

import "testing"

func TestCurrentPreservesArtifactIdentity(t *testing.T) {
	oldTime, oldRevision := BuiltAt, Revision
	t.Cleanup(func() { BuiltAt, Revision = oldTime, oldRevision })
	Revision = "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct{ raw, utc string }{
		{"2026-10-16T01:18:00-05:00", "2026-10-16T06:18:00Z"},
		{"2026-10-16T00:00:01Z", "2026-10-16T00:00:01Z"},
		{"", ""},
		{"invalid", ""},
	} {
		BuiltAt = c.raw
		first, second := Current(), Current()
		if first != second || first.BuiltAt != c.utc || first.Revision != Revision {
			t.Fatalf("artifact identity for %q: %+v, %+v", c.raw, first, second)
		}
	}
}

package version

import "testing"

func TestShort(t *testing.T) {
	cases := []struct{ in, want string }{
		// Coolify supplies the full revision.
		{"c76dc07f1a2b3c4d5e6f708192a3b4c5d6e7f809", "c76dc07f1a2b"},
		// A local build may already pass a short one.
		{"c76dc07", "c76dc07"},
		{"", ""},
		{"exactlytwelve", "exactlytwel"[:11] + "v"},
	}
	for _, c := range cases {
		if got := short(c.in); got != c.want {
			t.Errorf("short(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if len(short("c76dc07f1a2b3c4d5e6f708192a3b4c5d6e7f809")) != 12 {
		t.Error("a full SHA should trim to 12 characters")
	}
}

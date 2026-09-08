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

func TestCommitFromEnv(t *testing.T) {
	// Mirrors what init does, without re-running package initialisation.
	pick := func(env map[string]string) string {
		for _, key := range commitEnvVars {
			if v := env[key]; v != "" {
				return short(v)
			}
		}
		return "unknown"
	}

	if got := pick(map[string]string{}); got != "unknown" {
		t.Errorf("no env set: got %q, want %q", got, "unknown")
	}
	if got := pick(map[string]string{"SOURCE_COMMIT": "c27b02bdeadbeefcafe0123456789abcdef01234"}); got != "c27b02bdeadb" {
		t.Errorf("full SHA: got %q", got)
	}
	// Earlier names win, so an explicit GIT_SHA does not shadow the platform's.
	if got := pick(map[string]string{"SOURCE_COMMIT": "aaaaaaa", "GIT_SHA": "bbbbbbb"}); got != "aaaaaaa" {
		t.Errorf("precedence: got %q, want the first listed name", got)
	}
}

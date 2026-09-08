package version

import (
	"os"
	"runtime/debug"
	"strings"
	"time"
)

// Commit is the git SHA this binary was built from.
//
// Injected at link time:
//
//	go build -ldflags "-X geocoding-api/version.Commit=$(git rev-parse --short HEAD)"
//
// The Dockerfile wires that to a GIT_SHA build arg. When it is not supplied we
// fall back to the VCS stamp Go embeds automatically, which is present for a
// plain `go build` in a checkout but never inside the container: .dockerignore
// excludes the 186 MB .git directory, and adding it back would be paid on
// every build.
var Commit = ""

// Started is when this process began.
//
// This is the one deploy signal that needs no build configuration whatsoever.
// A redeploy restarts the container, so Started moves; comparing it before and
// after a push answers "did my change actually ship" without a key, a build
// arg, or anything else being set up correctly.
var Started = time.Now().UTC()

// commitEnvVars are read at startup when no commit was baked in at link time.
//
// Coolify does not pass SOURCE_COMMIT as a build argument -- verified against
// the live deploy, where the ldflags fallback still produced "unknown" -- but
// it does expose deployment metadata to the container environment, so the same
// value may be available at runtime instead. Costs one map lookup at startup
// and cannot report anything worse than "unknown".
var commitEnvVars = []string{"SOURCE_COMMIT", "COMMIT_SHA", "GIT_SHA", "GIT_COMMIT"}

func init() {
	if Commit != "" {
		Commit = short(Commit)
		return
	}

	for _, key := range commitEnvVars {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			Commit = short(v)
			return
		}
	}

	Commit = "unknown"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			Commit = short(s.Value)
			return
		}
	}
}

// short trims a full 40-character SHA to something readable. Coolify supplies
// the full revision; a local build may pass an already-short one.
func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

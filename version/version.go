package version

import (
	"runtime/debug"
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

func init() {
	if Commit != "" {
		return
	}
	Commit = "unknown"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			Commit = s.Value
			if len(Commit) > 12 {
				Commit = Commit[:12]
			}
			return
		}
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// The SPA fallback joins the request path onto a directory and hands it to
// the filesystem. Pasted on raw, it served whatever was above that
// directory: GET /../go.mod returned this repository's go.mod, and from the
// container -- working directory /app -- GET /../../etc/passwd was
// /etc/passwd. The route sits outside APIKeyAuth, so no key was needed.
func TestStaticFallbackCannotEscapeItsDirectory(t *testing.T) {
	// A static directory with one file in it, and a secret next to it that
	// stands in for everything above: go.mod, .env, /etc/passwd,
	// /proc/self/environ.
	root := t.TempDir()
	staticDir := filepath.Join(root, "static")
	if err := os.MkdirAll(filepath.Join(staticDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	const page = "<!doctype html><html>the app</html>"
	for path, body := range map[string]string{
		filepath.Join(staticDir, "index.html"):       page,
		filepath.Join(staticDir, "assets", "app.js"): "console.log(1)",
		filepath.Join(root, "secret.txt"):            "DB_PASSWORD=hunter2",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := echo.New()
	e.GET("/*", spaHandler(staticDir))

	serve := func(t *testing.T, target string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://example.test", nil)
		// Set the path directly: the traversal is in the path, and routing
		// it through a URL parser would clean some of these before the
		// handler ever saw them, which is the mistake being tested for.
		req.URL.Path = target
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	escapes := []string{
		"/../secret.txt",
		"/../../secret.txt",
		"/assets/../../secret.txt",
		"/./../secret.txt",
		"/..//secret.txt",
		"/a/b/../../../secret.txt",
		"/../" + filepath.Base(root) + "/secret.txt",
	}
	for _, target := range escapes {
		t.Run(target, func(t *testing.T) {
			rec := serve(t, target)
			if strings.Contains(rec.Body.String(), "hunter2") {
				t.Fatalf("served a file from outside the static directory: %s", rec.Body.String())
			}
			// It falls back to the app, as any unknown path does.
			if rec.Body.String() != page {
				t.Errorf("body = %q, want the SPA page", rec.Body.String())
			}
		})
	}

	// And the files it is meant to serve still reach the client.
	for target, want := range map[string]string{
		"/":              page,
		"/index.html":    page,
		"/assets/app.js": "console.log(1)",
		"/dashboard":     page, // a client-side route
	} {
		rec := serve(t, target)
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s -> %d %q, want %q", target, rec.Code, rec.Body.String(), want)
		}
	}

	// API paths stay with the API's own 404, not the SPA page.
	if rec := serve(t, "/api/v1/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("/api/v1/nope -> %d, want 404", rec.Code)
	}
}

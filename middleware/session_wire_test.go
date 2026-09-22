package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"geocoding-api/services"

	"github.com/labstack/echo/v4"
)

// A session token on an endpoint that does not bill by session is refused
// rather than ignored: a caller who thinks they are in a session and is
// being billed per call should be told.
func TestSessionTokenOnTheWrongEndpoint(t *testing.T) {
	for _, path := range []string{"/api/v1/geocode/43215", "/api/v1/reverse", "/api/v1/addresses"} {
		if services.SessionEligible(path) {
			t.Errorf("%s should not accept a session token", path)
		}
	}
}

// Malformed tokens are refused before they reach the tracker: the token ends
// up in a map key and in logs.
func TestSessionTokenShapes(t *testing.T) {
	if !services.ValidSessionToken("8f14e45f-ea8d-4c1b-9b47-1f4b0ac96b23") {
		t.Error("a UUID was rejected")
	}
	for _, bad := range []string{"short", "with space", strings.Repeat("x", 65)} {
		if services.ValidSessionToken(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// The headers a client paces itself with have to reach the wire, not just
// the recorder: this codebase has set headers after the body before now.
func TestSessionHeadersReachTheClient(t *testing.T) {
	state := services.SessionState{Billed: false, Remaining: 17, ExpiresIn: 119 * 1e9}

	e := echo.New()
	e.GET("/api/v1/addresses/search", func(c echo.Context) error {
		setSessionHeaders(c, state)
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	})
	srv := httptest.NewServer(e)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/addresses/search?q=main&session=8f14e45f-ea8d-4c1b")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for header, want := range map[string]string{
		"X-Session-Billed":          "false",
		"X-Session-Calls-Remaining": "17",
		"X-Session-Expires-In":      "119",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

const sessionBillingSchema = "session_billing_probe"

// What the feature is for, end to end: the keystrokes of one lookup reach
// usage_records as a single billed call.
//
// The unit tests prove the tracker decides correctly; this proves the
// decision is the one the billing path uses. Without it, a middleware that
// computed the right answer and then recorded every call as billable anyway
// would pass everything else.
func TestSessionCallsAreRecordedUnbilled(t *testing.T) {
	db := setupKeyFixture(t, sessionBillingSchema)
	const secret, otherSecret = probeSecret, probeOtherSecret

	e := echo.New()
	e.Use(APIKeyAuth())
	e.GET("/api/v1/addresses/search", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	})

	callAs := func(t *testing.T, key, url string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("X-API-Key", key)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", url, rec.Code, rec.Body.String())
		}
		return rec
	}
	call := func(t *testing.T, url string) *httptest.ResponseRecorder {
		t.Helper()
		return callAs(t, secret, url)
	}

	const token = "session-billing-probe-token"
	first := call(t, "/api/v1/addresses/search?q=ma&session="+token)
	if got := first.Header().Get("X-Session-Billed"); got != "true" {
		t.Errorf("first call X-Session-Billed = %q, want true", got)
	}
	for i := 0; i < 4; i++ {
		rec := call(t, fmt.Sprintf("/api/v1/addresses/search?q=main%d&session=%s", i, token))
		if got := rec.Header().Get("X-Session-Billed"); got != "false" {
			t.Errorf("keystroke %d X-Session-Billed = %q, want false", i+2, got)
		}
	}
	// The same token from another customer's key: a session belongs to the
	// key that opened it, so this is a new lookup and is billed.
	other := callAs(t, otherSecret, "/api/v1/addresses/search?q=ma&session="+token)
	if got := other.Header().Get("X-Session-Billed"); got != "true" {
		t.Errorf("another key's call X-Session-Billed = %q, want true -- it rode the first key's session", got)
	}

	// One more lookup, no token: billed like any other call.
	call(t, "/api/v1/addresses/search?q=elm")

	// RecordUsage runs after the response, so the rows arrive shortly after.
	var billed, unbilled int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.QueryRow(`SELECT
			count(*) FILTER (WHERE billable),
			count(*) FILTER (WHERE NOT billable) FROM usage_records`).Scan(&billed, &unbilled); err != nil {
			t.Fatal(err)
		}
		if billed+unbilled == 7 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if billed != 3 || unbilled != 4 {
		t.Errorf("recorded %d billed and %d free, want 3 and 4: five keystrokes are one lookup, plus another key's call and one ordinary call",
			billed, unbilled)
	}

	// The quota counts the lookups, not the keystrokes.
	var counted int
	if err := db.QueryRow(`SELECT COALESCE(SUM(count), 0) FROM usage_counters WHERE period_kind = 'month'`).Scan(&counted); err != nil {
		t.Fatal(err)
	}
	// Two for this key's owner: the session and the plain call. The other
	// customer's single call is counted against their own user.
	if counted != 3 {
		t.Errorf("quota counted %d calls across both users, want 3", counted)
	}
}

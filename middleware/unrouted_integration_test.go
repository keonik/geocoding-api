package middleware

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// A request that matched no route must keep its 404, even with a valid key.
//
// Answering 403 tells a caller their key lacks a permission for an endpoint
// that does not exist, which hides the 404 that says what is actually wrong.
// The route table did exactly that when first written: echo registers
// catch-alls inside a group carrying middleware, so an unmatched path reaches
// the scope check with no scope to find.
//
// This needs a real key: without one the auth layer answers 401 first and the
// permission check never runs, which is why the version of this test that did
// not use a key could not see the bug.
func TestUnroutedRequestsKeepTheir404(t *testing.T) {
	db := setupKeyFixture(t, "unrouted_probe")

	e := echo.New()
	api := e.Group("/api/v1")
	protected := api.Group("")
	protected.Use(APIKeyAuth())
	// One real route, so the group exists and the key has a scope for it.
	protected.GET("/counties", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	})

	call := func(t *testing.T, method, path string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-API-Key", probeSecret)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	// The real route still works, so the key and its scope are good.
	if code := call(t, http.MethodGet, "/api/v1/counties"); code != http.StatusOK {
		t.Fatalf("the real route answered %d; the fixture key is wrong", code)
	}

	for _, r := range []struct{ method, path, what string }{
		{http.MethodGet, "/api/v1/no-such-endpoint", "an endpoint that does not exist"},
		{http.MethodGet, "/api/v1/counties/extra/segments", "extra path segments"},
		{http.MethodPost, "/api/v1/counties", "the wrong verb for a real route"},
	} {
		t.Run(r.what, func(t *testing.T) {
			code := call(t, r.method, r.path)
			if code == http.StatusForbidden {
				t.Errorf("%s %s -> 403; %s should not answer about permissions", r.method, r.path, r.what)
			}
			if code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s -> %d, want 404 or 405", r.method, r.path, code)
			}
		})
	}

	// Usage is recorded in a goroutine after each response. Wait for them:
	// the fixture's cleanup closes this database and puts the global handle
	// back, and a goroutine still running then writes through a nil handle
	// and panics.
	waitForUsageRows(t, db, 4)
}

// waitForUsageRows blocks until at least n usage rows have landed.
func waitForUsageRows(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var rows int
		if err := db.QueryRow(`SELECT count(*) FROM usage_records`).Scan(&rows); err != nil {
			t.Fatalf("count usage rows: %v", err)
		}
		if rows >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("usage rows did not reach %d; a recording goroutine may outlive the test", n)
}

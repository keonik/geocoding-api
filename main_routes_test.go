package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"geocoding-api/middleware"

	"github.com/labstack/echo/v4"
)

// protectedRoutes are the routes APIKeyAuth guards, as the protected group
// recorded them while registering. Taken from the registration rather than
// inferred from path shapes: a protected route under /user or /admin would
// look exempt to a prefix filter and escape the check below.
func protectedRoutes(t *testing.T) []string {
	t.Helper()
	protectedRoutePaths = nil
	registerRoutes(echo.New(), "static")
	if len(protectedRoutePaths) == 0 {
		t.Fatal("no protected routes recorded; has the router moved?")
	}
	return protectedRoutePaths
}

// Every endpoint an API key can reach must say which scope it needs.
//
// This is the reason the table exists. The substring ladder it replaced could
// not be checked against the routes at all: a new endpoint got whichever
// scope its path happened to resemble, and nothing failed if that was the
// wrong one or none at all.
func TestEveryProtectedRouteDeclaresAScope(t *testing.T) {
	for _, route := range protectedRoutes(t) {
		layer := ""
		if strings.Contains(route, ":layer") {
			// Tiles carry their scope in the layer; the layers themselves are
			// covered in the middleware tests.
			layer = "counties"
		}
		if scope, ok := middleware.ScopeForRoute(route, layer); !ok {
			t.Errorf("%s has no scope: add it to routeScopes, or it is closed to every key", route)
		} else if scope == "" {
			t.Errorf("%s declares an empty scope", route)
		}
	}
}

// And the table must not accumulate entries for routes that no longer exist,
// which would quietly keep a deleted endpoint's rules alive.
func TestScopeTableHasNoStaleRoutes(t *testing.T) {
	live := map[string]bool{}
	for _, route := range protectedRoutes(t) {
		live[route] = true
	}
	for _, route := range middleware.ScopedRoutes() {
		if !live[route] {
			t.Errorf("routeScopes names %s, which is not a registered route", route)
		}
	}
}

// The counter rebuild is only a safety net if something can call it. It was
// unreachable when first written -- exported, tested, and wired to nothing --
// which would have left counter drift permanent in production.
//
// Asserted against the routing table rather than by grepping main.go for the
// text of a registration, which is what this used to do and what broke when
// the routes moved to their own file.
func TestAdminRebuildRouteIsRegistered(t *testing.T) {
	e := echo.New()
	registerRoutes(e, "static")

	const want = "/api/v1/admin/usage-counters/rebuild"
	for _, r := range e.Routes() {
		if r.Path == want {
			if r.Method != "POST" {
				t.Errorf("rebuild is registered as %s, want POST", r.Method)
			}
			// On the admin group: rebuilding recomputes every user's
			// counters, so an ordinary caller must not reach it.
			if !strings.HasPrefix(r.Path, "/api/v1/admin/") {
				t.Errorf("rebuild sits at %s, outside the admin group", r.Path)
			}
			return
		}
	}
	t.Errorf("no route at %s: counter drift would be uncorrectable", want)
}

// A request that matches no route must keep its 404. Telling a caller their
// key lacks a permission for an endpoint that does not exist hides what is
// actually wrong, and the route table did exactly that at first: echo
// registers catch-alls inside a group that carries middleware, so an
// unmatched path reaches the scope check with no scope to find.
func TestUnmatchedPathsKeepTheir404(t *testing.T) {
	e := echo.New()
	registerRoutes(e, "static")

	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/no-such-endpoint"},
		{http.MethodGet, "/api/v1/counties/franklin/boundary/extra"},
		{http.MethodPost, "/api/v1/counties"}, // wrong verb for a real route
		{http.MethodGet, "/api/v1/"},
	} {
		req := httptest.NewRequest(r.method, r.path, nil)
		// No key: an unmatched path should not be reporting on permissions,
		// and 401 here would be the auth layer answering before routing.
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s %s -> 403; an unmatched path should not answer about permissions", r.method, r.path)
		}
	}
}

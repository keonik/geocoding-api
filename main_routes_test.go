package main

import (
	"strings"
	"testing"

	"geocoding-api/middleware"

	"github.com/labstack/echo/v4"
)

// protectedRoutes are the ones APIKeyAuth guards: everything under /api/v1
// except health, the auth endpoints, and the two groups that authenticate
// with a JWT instead of a key.
func protectedRoutes(t *testing.T) []echo.Route {
	t.Helper()
	e := echo.New()
	registerRoutes(e, "static")

	var routes []echo.Route
	for _, r := range e.Routes() {
		rest, ok := strings.CutPrefix(r.Path, "/api/v1/")
		if !ok {
			continue
		}
		// Echo registers internal entries for its own not-found handling;
		// they are not endpoints anybody can call.
		if r.Method == "echo_route_not_found" {
			continue
		}
		switch {
		case rest == "health",
			strings.HasPrefix(rest, "auth/"),
			strings.HasPrefix(rest, "user/"),
			strings.HasPrefix(rest, "admin/"):
			continue
		}
		routes = append(routes, *r)
	}
	if len(routes) == 0 {
		t.Fatal("no protected routes found; has the router moved?")
	}
	return routes
}

// Every endpoint an API key can reach must say which scope it needs.
//
// This is the reason the table exists. The substring ladder it replaced could
// not be checked against the routes at all: a new endpoint got whichever
// scope its path happened to resemble, and nothing failed if that was the
// wrong one or none at all.
func TestEveryProtectedRouteDeclaresAScope(t *testing.T) {
	for _, r := range protectedRoutes(t) {
		layer := ""
		if strings.Contains(r.Path, ":layer") {
			// Tiles carry their scope in the layer; the layers themselves are
			// covered in the middleware tests.
			layer = "counties"
		}
		if scope, ok := middleware.ScopeForRoute(r.Path, layer); !ok {
			t.Errorf("%s %s has no scope: add it to routeScopes, or it is closed to every key",
				r.Method, r.Path)
		} else if scope == "" {
			t.Errorf("%s %s declares an empty scope", r.Method, r.Path)
		}
	}
}

// And the table must not accumulate entries for routes that no longer exist,
// which would quietly keep a deleted endpoint's rules alive.
func TestScopeTableHasNoStaleRoutes(t *testing.T) {
	live := map[string]bool{}
	for _, r := range protectedRoutes(t) {
		live[r.Path] = true
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

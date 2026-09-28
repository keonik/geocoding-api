package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// Every route an API key can reach, and the scope it demands. Written out
// rather than derived, so a change to the table is a change to this list and
// somebody has to agree to it: these are the rules that decide whether an
// issued key still works.
func TestRouteScopes(t *testing.T) {
	cases := map[string]string{
		"/api/v1/coverage":                    "coverage",
		"/api/v1/geocode/:zipcode":            "geocode",
		"/api/v1/geocode/batch":               "geocode",
		"/api/v1/reverse":                     "reverse",
		"/api/v1/reverse/batch":               "reverse",
		"/api/v1/enrich":                      "enrich",
		"/api/v1/search":                      "search",
		"/api/v1/distance/:from/:to":          "distance",
		"/api/v1/nearby/:zipcode":             "nearby",
		"/api/v1/proximity/:center/:target":   "proximity",
		"/api/v1/addresses":                   "addresses",
		"/api/v1/addresses/:id":               "addresses",
		"/api/v1/address/validate":            "addresses",
		"/api/v1/counties":                    "counties",
		"/api/v1/counties/:name":              "counties",
		"/api/v1/counties/:name/boundary":     "counties",
		"/api/v1/cities":                      "cities",
		"/api/v1/cities/:id":                  "cities",
		"/api/v1/cities/zips":                 "cities",
		"/api/v1/states":                      "states",
		"/api/v1/states/lookup":               "states",
		"/api/v1/states/:identifier":          "states",
		"/api/v1/states/:identifier/boundary": "states",

		// Inherited oddities, kept deliberately: the old substring ladder
		// checked "/search" before anything else, so these two demand the
		// search scope rather than the one their path suggests. Moving them
		// would change which issued keys work.
		"/api/v1/addresses/search":       "search",
		"/api/v1/counties/bounds/search": "search",
	}
	for route, want := range cases {
		got, ok := ScopeForRoute(route, "")
		if !ok {
			t.Errorf("%s has no scope", route)
			continue
		}
		if got != want {
			t.Errorf("%s demands %q, want %q", route, got, want)
		}
	}
}

// A tile's scope comes from the layer it asks for, which the route pattern
// does not contain: county tiles are county data.
func TestTileScopeFollowsTheLayer(t *testing.T) {
	for layer, want := range map[string]string{"counties": "counties", "states": "states"} {
		got, ok := ScopeForRoute(tileRoute, layer)
		if !ok || got != want {
			t.Errorf("tiles/%s demands %q (ok=%v), want %q", layer, got, ok, want)
		}
	}
	// A layer nobody has named yet is refused rather than given whichever
	// scope its name happens to contain.
	if scope, ok := ScopeForRoute(tileRoute, "parcels"); ok {
		t.Errorf("an unlisted tile layer resolved to %q", scope)
	}
}

// An unmapped route is refused. A new endpoint is closed until it says what
// it needs, rather than open because its path resembles something else.
func TestUnmappedRouteIsRefused(t *testing.T) {
	for _, route := range []string{"/api/v1/something-new", "/api/v1/admin/boundaries", ""} {
		if scope, ok := ScopeForRoute(route, ""); ok {
			t.Errorf("%s resolved to %q", route, scope)
		}
	}
}

// The scope is read from the route echo matched, not from the URL, so a
// parameter value cannot change which permission is demanded. Under the old
// substring match, a ZIP code or county name containing "search" did exactly
// that.
func TestScopeComesFromTheRouteNotTheURL(t *testing.T) {
	e := echo.New()
	var got string
	e.GET("/api/v1/counties/:name", func(c echo.Context) error {
		got = endpointFor(c)
		return c.NoContent(http.StatusOK)
	})

	// A county whose name contains another endpoint's word.
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/counties/search", nil))
	if got != "counties" {
		t.Errorf("county named \"search\" demanded %q, want counties", got)
	}
}

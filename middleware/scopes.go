package middleware

import "github.com/labstack/echo/v4"

// routeScopes says which permission scope each API-key route demands, keyed
// by the route as echo registered it.
//
// This used to be a ladder of strings.Contains over the request path, which
// gave the right answer mostly by coincidence and by the order the checks
// happened to be written in. Two of the answers below are that coincidence
// showing: /addresses/search demands "search" rather than "addresses", and
// /counties/bounds/search demands "search" rather than "counties", because
// the "/search" check sat above the others. They are kept exactly as they
// were -- an issued key either carries the scope it has been using or it does
// not, and silently moving the requirement would lock working integrations
// out or open ones that were closed. They are written down here so the next
// person can decide deliberately rather than inherit an accident.
//
// A route missing from this table resolves to no scope and is refused, so a
// new endpoint is closed until it says what it needs. The test in package
// main walks the routing table and fails if one is missing, which is the
// point of writing it down: the old ladder could not be checked against the
// routes at all.
var routeScopes = map[string]string{
	"/api/v1/coverage": "coverage",

	"/api/v1/geocode/:zipcode": "geocode",
	"/api/v1/geocode/batch":    "geocode",
	"/api/v1/reverse":          "reverse",
	"/api/v1/enrich":           "enrich",
	"/api/v1/search":           "search",

	"/api/v1/distance/:from/:to":        "distance",
	"/api/v1/nearby/:zipcode":           "nearby",
	"/api/v1/proximity/:center/:target": "proximity",

	"/api/v1/addresses":        "addresses",
	"/api/v1/addresses/:id":    "addresses",
	"/api/v1/addresses/search": "search", // see above
	"/api/v1/address/validate": "addresses",

	"/api/v1/counties":                "counties",
	"/api/v1/counties/:name":          "counties",
	"/api/v1/counties/:name/boundary": "counties",
	"/api/v1/counties/bounds/search":  "search", // see above

	"/api/v1/cities":      "cities",
	"/api/v1/cities/:id":  "cities",
	"/api/v1/cities/zips": "cities",

	"/api/v1/states":                      "states",
	"/api/v1/states/lookup":               "states",
	"/api/v1/states/:identifier":          "states",
	"/api/v1/states/:identifier/boundary": "states",
}

// tileRoute is the one route whose scope is not fixed by its pattern: a tile
// carries the layer in the path, and a caller asking for county tiles needs
// the county scope. Reading it from the parameter is what the old substring
// match was doing by accident, and what a reader would expect it to do.
const tileRoute = "/api/v1/tiles/:layer/:z/:x/:y"

// tileLayerScopes maps a tile layer to the scope its data needs. A layer that
// is not listed has no scope, so a new layer is refused until it is named
// here rather than inheriting whichever scope its name happens to contain.
var tileLayerScopes = map[string]string{
	"counties": "counties",
	"states":   "states",
}

// ScopeForRoute returns the scope a route demands. ok is false when the route
// is not one an API key can reach, which callers treat as a refusal.
func ScopeForRoute(route, tileLayer string) (string, bool) {
	if route == tileRoute {
		scope, ok := tileLayerScopes[tileLayer]
		return scope, ok
	}
	scope, ok := routeScopes[route]
	return scope, ok
}

// endpointFor is the scope for the request in hand.
//
// c.Path() is the route echo matched, not the URL: "/api/v1/geocode/:zipcode"
// rather than "/api/v1/geocode/43215". That is what makes this a lookup
// instead of a guess -- there is one entry per route, and a ZIP code that
// happens to contain the word "search" cannot change the answer.
func endpointFor(c echo.Context) string {
	scope, ok := ScopeForRoute(c.Path(), c.Param("layer"))
	if !ok {
		return "unknown"
	}
	return scope
}

// ScopedRoutes lists the routes the table names, so a test can check it
// against the routes that actually exist.
func ScopedRoutes() []string {
	routes := make([]string, 0, len(routeScopes)+1)
	for route := range routeScopes {
		routes = append(routes, route)
	}
	return append(routes, tileRoute)
}

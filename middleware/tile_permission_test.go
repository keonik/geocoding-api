package middleware

import "testing"

// Tile paths resolve to a scope through getEndpointName's substring matching:
// /tiles/counties/... contains "/counties" and so takes the counties scope,
// /tiles/states/... takes states. That is the behaviour we want, but it falls
// out of substring coincidence rather than anything explicit -- rename a layer
// and the permission it demands changes silently.
//
// Pinning it here means a rename breaks a test instead of quietly widening
// what a key can reach.
func TestTilePathsTakeTheirLayersScope(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/tiles/counties/8/70/97.mvt", "counties"},
		{"/api/v1/tiles/states/6/17/24.mvt", "states"},
		// The endpoints the tiles sit alongside, so a change to the ordering
		// in getEndpointName shows up here too.
		{"/api/v1/counties/Franklin/boundary", "counties"},
		{"/api/v1/states/OH/boundary", "states"},
		{"/api/v1/enrich", "enrich"},
		{"/api/v1/reverse", "reverse"},
	}

	for _, tc := range cases {
		if got := getEndpointName(tc.path); got != tc.want {
			t.Errorf("getEndpointName(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// An unmapped path returns "unknown", which HasPermission refuses. A new
// endpoint that forgets to add itself is therefore closed by default rather
// than open, and this records that.
func TestUnmappedPathIsUnknown(t *testing.T) {
	if got := getEndpointName("/api/v1/something-new"); got != "unknown" {
		t.Errorf("an unmapped path resolved to %q; new endpoints should be closed by default", got)
	}
}

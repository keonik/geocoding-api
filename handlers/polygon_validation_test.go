package handlers

import (
	"fmt"
	"strings"
	"testing"
)

// A malformed shape must be a 400 naming the problem, not a 500 from GEOS
// deep inside ST_Intersects. Holes are the gap that mattered: validating only
// coordinates[0] let a short or unclosed hole through.
func TestPolygonValidationRejectsMalformedShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "unclosed exterior ring",
			raw:  `{"type":"Polygon","coordinates":[[[-83.1,39.9],[-82.9,39.9],[-82.9,40.1]]]}`,
			want: "exterior ring",
		},
		{
			name: "short hole",
			raw: `{"type":"Polygon","coordinates":[` +
				`[[-83.1,39.9],[-82.9,39.9],[-82.9,40.1],[-83.1,39.9]],` +
				`[[-83.0,40.0],[-82.95,40.0]]]}`,
			want: "hole 1",
		},
		{
			name: "unclosed hole",
			raw: `{"type":"Polygon","coordinates":[` +
				`[[-83.1,39.9],[-82.9,39.9],[-82.9,40.1],[-83.1,39.9]],` +
				`[[-83.0,40.0],[-82.95,40.0],[-82.95,40.05],[-83.0,40.01]]]}`,
			want: "hole 1",
		},
		{
			// Only catchable when the swap pushes a value out of range. A
			// latitude-first pair whose numbers both happen to be legal
			// coordinates is indistinguishable from a deliberate query for
			// somewhere else on Earth, and the validator does not pretend
			// otherwise -- it reports the range, and names the convention so
			// the caller can spot the swap themselves.
			name: "coordinates out of range",
			raw:  `{"type":"Polygon","coordinates":[[[39.9,-183.1],[39.9,-82.9],[40.1,-82.9],[39.9,-183.1]]]}`,
			want: "longitude first",
		},
		{
			name: "wrong geometry type",
			raw:  `{"type":"Point","coordinates":[-83.0,40.0]}`,
			want: "only Polygon",
		},
		{
			name: "not json",
			raw:  `not json at all`,
			want: "valid GeoJSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGeoJSONPolygon(tc.raw)
			if err == nil {
				t.Fatalf("accepted; GEOS would reject it as a 500 instead of a 400")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q, so it does not tell the caller what to fix", err, tc.want)
			}
		})
	}
}

// An exact point-in-polygon test runs per candidate row, so an unbounded
// vertex count lets one request cost seconds.
func TestPolygonVertexCountIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"type":"Polygon","coordinates":[[`)
	n := maxPolygonVertices + 10
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "[%f,%f]", -83.0+float64(i)*1e-6, 40.0)
	}
	b.WriteString(`,[-83.0,40.0]]]}`)

	if err := validateGeoJSONPolygon(b.String()); err == nil {
		t.Error("a polygon with more than the allowed vertices was accepted")
	}
}

// A well-formed polygon with a hole has to still be accepted -- the point of
// validating every ring is correctness, not refusing legitimate shapes.
func TestPolygonWithValidHoleIsAccepted(t *testing.T) {
	raw := `{"type":"Polygon","coordinates":[` +
		`[[-83.1,39.9],[-82.9,39.9],[-82.9,40.1],[-83.1,40.1],[-83.1,39.9]],` +
		`[[-83.05,39.95],[-82.95,39.95],[-82.95,40.05],[-83.05,40.05],[-83.05,39.95]]]}`
	if err := validateGeoJSONPolygon(raw); err != nil {
		t.Errorf("a valid polygon with a hole was rejected: %v", err)
	}
}

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// These are rejected before any query runs, so they need no database.
func TestEnrichAndReverseRejectBadInput(t *testing.T) {
	cases := []struct {
		name, url, wantErr string
		handler            echo.HandlerFunc
	}{
		{"enrich without lng", "/enrich?lat=39.96", "Both lat and lng are required", EnrichHandler},
		{"enrich with a bad lat", "/enrich?lat=north&lng=-83", "lat is not a number", EnrichHandler},
		{"enrich with an unknown field", "/enrich?lat=39.96&lng=-83&fields=census,zodiac", "unknown fields zodiac", EnrichHandler},
		{"reverse with an unknown field", "/reverse?lat=39.96&lng=-83&fields=zodiac", "unknown fields zodiac", ReverseGeocodeHandler},
		{"reverse without lat", "/reverse?lng=-83", "Both lat and lng are required", ReverseGeocodeHandler},
		{"enrich with lng out of range", "/enrich?lat=39.96&lng=-183", "lng -183 is outside", EnrichHandler},
		{"enrich with lat out of range", "/enrich?lat=91&lng=-83", "lat 91 is outside", EnrichHandler},
		{"reverse with NaN", "/reverse?lat=NaN&lng=-83", "lat is not a number", ReverseGeocodeHandler},
	}
	e := echo.New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c := e.NewContext(httptest.NewRequest(http.MethodGet, tc.url, nil), rec)
			if err := tc.handler(c); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Errorf("body %s, want it to mention %q", rec.Body.String(), tc.wantErr)
			}
		})
	}
}

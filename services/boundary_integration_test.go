package services

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"geocoding-api/database"

	_ "github.com/lib/pq"
)

// Exercises the three tolerance paths of the boundary endpoints against real
// PostGIS geometry. The point of the test is the parameter numbering: each
// path builds a different SQL string, and handing a statement an argument it
// never references fails to parse rather than being ignored.
// Skipped unless PROBE_DSN is set.
func TestBoundaryGeometryProbe(t *testing.T) {
	dsn := os.Getenv("PROBE_DSN")
	if dsn == "" {
		t.Skip("PROBE_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	prev := database.DB
	database.DB = db
	defer func() { database.DB = prev }()

	requireTables(t, db, "us_states", "ohio_counties")
	requireColumns(t, db, "us_states", "geometry", "geometry_simplified")
	requireColumns(t, db, "ohio_counties", "bounds_geometry_simplified")

	ss := &StateService{}
	cs := &CountyService{db: db}

	sizeOf := func(t *testing.T, feature map[string]interface{}) int {
		t.Helper()
		geom, ok := feature["geometry"]
		if !ok {
			t.Fatal("feature has no geometry member")
		}
		raw, ok := geom.(json.RawMessage)
		if !ok {
			t.Fatalf("geometry is %T, want json.RawMessage (passthrough, not re-encoded)", geom)
		}
		var probe struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			t.Fatalf("geometry is not valid GeoJSON: %v", err)
		}
		if probe.Type == "" || len(probe.Coordinates) == 0 {
			t.Fatalf("geometry has no type/coordinates: %s", truncate(raw))
		}
		return len(raw)
	}

	var dflt, full, custom int

	t.Run("default tolerance reads the precomputed column", func(t *testing.T) {
		f, err := ss.GetStateBoundaryGeoJSON("TX", DefaultBoundaryTolerance, 6)
		if err != nil {
			t.Fatalf("default: %v", err)
		}
		dflt = sizeOf(t, f)
	})

	t.Run("tolerance=0 returns full resolution", func(t *testing.T) {
		f, err := ss.GetStateBoundaryGeoJSON("TX", 0, 6)
		if err != nil {
			t.Fatalf("full: %v", err)
		}
		full = sizeOf(t, f)
	})

	t.Run("custom tolerance simplifies on the fly", func(t *testing.T) {
		// This is the only path that binds a tolerance placeholder.
		f, err := ss.GetStateBoundaryGeoJSON("TX", 0.01, 6)
		if err != nil {
			t.Fatalf("custom: %v", err)
		}
		custom = sizeOf(t, f)
	})

	if !(custom < dflt && dflt < full) {
		t.Errorf("expected custom(0.01) < default(0.0005) < full(0); got %d, %d, %d", custom, dflt, full)
	}
	// Measured ~8.9x at precision 6 on real TIGER geometry. Guard the order of
	// magnitude, not the exact figure, which moves with the source data.
	if full < 5*dflt {
		t.Errorf("default should be several times smaller than full: full=%d default=%d", full, dflt)
	}
	t.Logf("TX geometry bytes — full: %d, default: %d, tolerance=0.01: %d", full, dflt, custom)

	t.Run("county boundary returns real geometry", func(t *testing.T) {
		b, err := cs.GetCountyBoundaryGeoJSON("Franklin", DefaultBoundaryTolerance, 6)
		if err != nil {
			t.Fatalf("county: %v", err)
		}
		if len(b.Features) != 1 {
			t.Fatalf("want 1 feature, got %d", len(b.Features))
		}
		// Regression guard: this used to return an empty coordinates array
		// while still paying to serialise the polygon.
		var probe struct {
			Coordinates [][][]float64 `json:"coordinates"`
		}
		if err := json.Unmarshal(b.Features[0].Geometry, &probe); err != nil {
			t.Fatalf("county geometry invalid: %v", err)
		}
		// The bug returned "coordinates": [] while still paying to serialise
		// the polygon, so assert a closed ring rather than a size.
		if len(probe.Coordinates) == 0 || len(probe.Coordinates[0]) < 4 {
			t.Fatalf("county geometry is not a ring: %s", truncate(b.Features[0].Geometry))
		}
	})
}

func truncate(b []byte) string {
	if len(b) > 120 {
		return string(b[:120]) + "..."
	}
	return string(b)
}
